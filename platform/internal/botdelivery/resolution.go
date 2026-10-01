package botdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const resolutionCleanupTimeout = 5 * time.Second

func captureAttempt(ctx context.Context, tx pgx.Tx, i Intent, p PreparedAttempt) error {
	expected := "sendMessage"
	if i.Phase == phaseEdit {
		expected = "editMessageText"
	}
	if i.Phase == string(DocumentIntent) {
		expected = "sendDocument"
	}
	if !p.valid() || p.Method != expected {
		return ErrBinding
	}
	raw, err := json.Marshal(p.Continuation)
	if err != nil {
		return ErrBinding
	}
	err = dbgen.New(tx).InsertBotDeliveryAttempt(ctx, dbgen.InsertBotDeliveryAttemptParams{
		BotID: i.BotID, OperationKey: i.Operation, EffectKey: i.Effect, Attempt: i.Attempt,
		Method: p.Method, PayloadSha256: p.SHA256, Continuation: raw,
	})
	return core.DatabaseOperationError(err)
}

func operatorAuthority(ctx context.Context, tx pgx.Tx, actor string, lock bool) error {
	if actor == "" || len(actor) > 256 {
		return ErrBinding
	}
	query := "SELECT owner FROM core.pass_booking_admins WHERE owner=$1"
	if lock {
		query += " FOR SHARE"
	}
	var found string
	err := tx.QueryRow(ctx, query, actor).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return core.DatabaseOperationError(err)
}

// Inspect is an operator-only exact-key read; it cannot render private sources.
func (s Service) Inspect(ctx context.Context, actor string, key IntentKey) (Inspection, error) {
	if !key.valid() || s.Delivery.Validate() != nil {
		return Inspection{}, ErrBinding
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Inspection{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = operatorAuthority(ctx, tx, actor, false); err != nil {
		return Inspection{}, err
	}
	return s.inspect(ctx, tx, key)
}

func (s Service) inspect(ctx context.Context, tx pgx.Tx, key IntentKey) (Inspection, error) {
	out := Inspection{IntentKey: key, BotID: s.Delivery.BotID, Disposition: "unresolved"}
	err := tx.QueryRow(ctx, `SELECT i.state,i.reason,i.attempt,i.phase,i.target_message_id,i.message_id,
 i.chat_id,COALESCE(a.method,''),
 a.payload_sha256 IS NOT NULL,COALESCE(a.payload_sha256,''),
 (SELECT count(*) FROM core.delivery_queue f WHERE f.bot_id=q.bot_id AND f.chat=q.chat
 AND f.lane_sequence>q.lane_sequence AND f.state IN ('pending','sending','unknown','parked','paused')),
 COALESCE((SELECT r.result->>'disposition' FROM bot.delivery_resolutions r
 WHERE r.result->>'bot_id'=i.bot_id::text AND r.request->>'operation'=i.operation_key
 AND r.request->>'effect'=i.effect_key AND r.request->>'attempt'=i.attempt::text
 ORDER BY r.created_at DESC LIMIT 1),'')
 FROM bot.delivery_intents i JOIN core.delivery_queue q ON q.bot_id=i.bot_id AND q.owner_kind='bot'
 AND q.owner_key=i.operation_key AND q.effect_key=i.effect_key
 LEFT JOIN bot.delivery_attempts a ON a.bot_id=i.bot_id AND a.operation_key=i.operation_key
 AND a.effect_key=i.effect_key AND a.attempt=i.attempt
 WHERE i.bot_id=$1 AND i.operation_key=$2 AND i.effect_key=$3`, s.Delivery.BotID, key.Operation, key.Effect).
		Scan(&out.State, &out.Reason, &out.Attempt, &out.Phase, &out.Target, &out.MessageID, &out.Chat, &out.Method, &out.Captured, &out.PayloadSHA256, &out.BlockedFollowers, &out.Disposition)
	if err != nil {
		return Inspection{}, core.DatabaseOperationError(err)
	}
	if out.State == delivery.Uncertain {
		out.Disposition = "unresolved"
	} else {
		if out.Disposition == "" {
			out.Disposition = "not_unknown"
		}
		out.BlockedFollowers = 0
	}
	return out, nil
}

func (r Resolution) valid() bool {
	if !r.IntentKey.valid() || r.Key == "" || len(r.Key) > 128 || r.Attempt <= 0 ||
		!r.Joined || !r.Quiescent || !validHash(r.EvidenceSHA256) || !validHash(r.PayloadSHA256) {
		return false
	}
	switch r.Disposition {
	case "confirmed_sent":
		return r.MessageID > 0 && r.EvidenceKind == "provider_receipt"
	case "confirmed_unsent":
		return r.MessageID == 0 && (r.EvidenceKind == "pre_dispatch_failure" || r.EvidenceKind == "complete_sink_proof")
	default:
		return false
	}
}

// Resolve requires a trusted stopped/joined operator and an evidence attestation.
// The exclusive bot lock prevents a new sender; it is not evidence of sink outcome.
func (s Service) Resolve(ctx context.Context, actor string, r Resolution) (Inspection, error) {
	if !r.valid() || s.Delivery.Validate() != nil {
		return Inspection{}, ErrBinding
	}
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return Inspection{}, core.DatabaseOperationError(err)
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(918273)").Scan(&locked); err != nil {
		return Inspection{}, core.DatabaseOperationError(err)
	}
	if !locked {
		return Inspection{}, ErrBinding
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), resolutionCleanupTimeout)
		defer cancel()
		var released bool
		if unlockErr := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock(918273)").
			Scan(&released); unlockErr != nil ||
			!released {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return Inspection{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = operatorAuthority(ctx, tx, actor, true); err != nil {
		return Inspection{}, err
	}
	return s.resolve(ctx, tx, actor, r)
}

func (s Service) resolve(ctx context.Context, tx pgx.Tx, actor string, r Resolution) (Inspection, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,81092))", actor+":"+r.Key); err != nil {
		return Inspection{}, core.DatabaseOperationError(err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return Inspection{}, ErrBinding
	}
	prior, err := s.priorResolution(ctx, tx, actor, r)
	if err == nil {
		return *prior, core.DatabaseOperationError(tx.Commit(ctx))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Inspection{}, err
	}
	q := dbgen.New(tx)
	ref := delivery.Reference{Owner: delivery.Bot, Key: r.Operation, Effect: r.Effect}
	i, err := Read(ctx, tx, s.Delivery.BotID, ref, true)
	if err != nil {
		return Inspection{}, err
	}
	if i.State != delivery.Uncertain || i.Attempt != r.Attempt {
		return Inspection{}, ErrBinding
	}
	saved, err := q.GetBotDeliveryAttempt(
		ctx,
		dbgen.GetBotDeliveryAttemptParams{
			BotID:        i.BotID,
			OperationKey: i.Operation,
			EffectKey:    i.Effect,
			Attempt:      i.Attempt,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Inspection{}, ErrBinding
		}
		return Inspection{}, core.DatabaseOperationError(err)
	}
	if saved.PayloadSha256 != r.PayloadSHA256 {
		return Inspection{}, ErrBinding
	}
	var continuation Continuation
	if json.Unmarshal(saved.Continuation, &continuation) != nil {
		return Inspection{}, ErrBinding
	}
	before, err := s.inspect(ctx, tx, r.IntentKey)
	if err != nil {
		return Inspection{}, err
	}
	outcome := delivery.Outcome{Kind: delivery.Deferred, Reason: "operator_confirmed_unsent", Missing: true}
	if r.Disposition == "confirmed_sent" {
		if i.Phase == phaseEdit && r.MessageID != i.Target {
			return Inspection{}, ErrBinding
		}
		outcome = delivery.Outcome{Kind: delivery.Succeeded, MessageID: r.MessageID}
	}
	if err = s.complete(ctx, tx, CompletionRequest{Attempt: i, Outcome: outcome, Receipt: continuation}); err != nil {
		return Inspection{}, err
	}
	out, err := s.inspect(ctx, tx, r.IntentKey)
	if err != nil {
		return Inspection{}, err
	}
	out.Disposition = r.Disposition
	if err = recordResolution(ctx, tx, actor, r.Key, raw, out, before); err != nil {
		return Inspection{}, err
	}
	return out, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) priorResolution(ctx context.Context, tx pgx.Tx, actor string, r Resolution) (*Inspection, error) {
	prior, err := dbgen.New(tx).GetBotDeliveryResolution(
		ctx, dbgen.GetBotDeliveryResolutionParams{Actor: actor, OperationKey: r.Key},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	var original Resolution
	var out Inspection
	if json.Unmarshal(prior.Request, &original) != nil || original != r ||
		json.Unmarshal(prior.Result, &out) != nil || out.BotID != s.Delivery.BotID {
		return nil, ErrBinding
	}
	return &out, nil
}

func recordResolution(ctx context.Context, tx pgx.Tx, actor, key string, request []byte, out, before Inspection) error {
	// Preserve original unknown reason and attempt in the audit result.
	audit := struct {
		Inspection

		OriginalState  delivery.Kind `json:"original_state"`
		OriginalReason string        `json:"original_reason"`
	}{out, before.State, before.Reason}
	result, err := json.Marshal(audit)
	if err != nil {
		return ErrBinding
	}
	return core.DatabaseOperationError(dbgen.New(tx).InsertBotDeliveryResolution(
		ctx, dbgen.InsertBotDeliveryResolutionParams{Actor: actor, OperationKey: key, Request: request, Result: result},
	),
	)
}
