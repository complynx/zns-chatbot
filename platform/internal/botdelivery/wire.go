package botdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

var ErrWireUnavailable = &core.ProblemError{Status: http.StatusConflict, Code: "original_wire_unavailable"}

// WireReference names trusted private bytes; it does not authorize delivery.
type WireReference struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}

// Wire is private runtime storage, never a host API payload or model input.
type Wire struct {
	Payload      telegram.Send `json:"payload"`
	Filename     string        `json:"filename,omitempty"`
	Body         []byte        `json:"body,omitempty"`
	Receipt      Continuation  `json:"receipt"`
	ExportEvents []string      `json:"export_events,omitempty"`
}

type wireBinding struct {
	BotID     int64     `json:"bot_id"`
	Owner     string    `json:"owner"`
	Chat      int64     `json:"chat"`
	Operation string    `json:"operation"`
	Effect    string    `json:"effect"`
	Reference Reference `json:"reference"`
}

type wireSnapshot struct {
	Binding         wireBinding `json:"binding"`
	Generation      int64       `json:"generation"`
	ObservedAttempt int64       `json:"observed_attempt"`
	AdmittedAttempt int64       `json:"admitted_attempt"`
	Phase           string      `json:"phase"`
	Target          int64       `json:"target"`
	Wire            Wire        `json:"wire"`
}

func wireIdentity(i Intent) wireBinding {
	return wireBinding{i.BotID, i.Owner, i.Chat, i.Operation, i.Effect, i.Reference}
}

func wireDigest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func wireCaptureState(ctx context.Context, db dbgen.DBTX, i Intent) (*WireReference, bool, error) {
	var key, hash *string
	var chain bool
	err := db.QueryRow(ctx, `SELECT wire_capture_key,wire_capture_hash,last_uncertain_attempt IS NOT NULL
 FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		i.BotID, i.Operation, i.Effect).Scan(&key, &hash, &chain)
	if err != nil {
		return nil, chain, core.DatabaseOperationContextError(ctx, err)
	}
	if key == nil && hash == nil {
		return nil, chain, nil
	}
	if key == nil || hash == nil {
		return nil, chain, ErrWireUnavailable
	}
	return &WireReference{Key: *key, Hash: *hash}, chain, nil
}

func readWireSnapshot(ctx context.Context, db dbgen.DBTX, i Intent, ref WireReference) (wireSnapshot, error) {
	var raw []byte
	var saved wireSnapshot
	generation, err := captureGeneration(i, ref)
	if err != nil {
		return saved, ErrWireUnavailable
	}
	if i.Owner != "" {
		var current int64
		if err = db.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM core.conversation_history_generations WHERE owner=$1),0)`, i.Owner).
			Scan(&current); err != nil {
			return saved, core.DatabaseOperationContextError(ctx, err)
		}
		if current != generation {
			return saved, ErrStale
		}
	}
	err = db.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=0 AND kind=$2`,
		i.Owner, ref.Key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return saved, ErrWireUnavailable
	}
	if err != nil {
		return saved, core.DatabaseOperationContextError(ctx, err)
	}
	if json.Unmarshal(raw, &saved) != nil || saved.Generation != generation ||
		!reflect.DeepEqual(saved.Binding, wireIdentity(i)) {
		return saved, ErrWireUnavailable
	}
	digest, err := wireDigest(saved.Wire)
	if err != nil || digest != ref.Hash {
		return saved, ErrWireUnavailable
	}
	return saved, nil
}

// RetryWire returns a captured uncertainty payload. Never-captured legacy rows
// may prepare their next authorized wire; a positive marker cannot fall back.
func RetryWire(ctx context.Context, db dbgen.DBTX, i Intent) (Wire, *WireReference, error) {
	ref, chain, err := wireCaptureState(ctx, db, i)
	if err != nil || ref == nil {
		return Wire{}, nil, err
	}
	saved, err := readWireSnapshot(ctx, db, i, *ref)
	if err != nil {
		return Wire{}, nil, err
	}
	if !chain {
		return Wire{}, nil, nil
	}
	if saved.AdmittedAttempt != i.Attempt {
		return Wire{}, nil, ErrWireUnavailable
	}
	return saved.Wire, ref, nil
}

// StageWire is a trusted worker's private write under the history fence. Only
// Begin binds it to Ready and checks current source/recipient authorization.
func (s Service) StageWire(ctx context.Context, observed Intent, wire Wire) (*WireReference, error) {
	if observed.BotID != s.Delivery.BotID {
		return nil, ErrBinding
	}
	if err := validateWire(observed, wire); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	generation, err := wireGeneration(ctx, tx, observed.Owner)
	if err != nil {
		return nil, err
	}
	current, err := Read(ctx, tx, observed.BotID, observed.QueueReference(), true)
	if err != nil {
		return nil, err
	}
	if !sameBinding(current, observed) || current.Attempt != observed.Attempt || current.State != delivery.Deferred {
		return nil, ErrBinding
	}
	if ref, chain, readErr := wireCaptureState(ctx, tx, current); readErr != nil || (chain && ref != nil) {
		return ref, readErr
	}
	ref, err := stageWireSnapshot(ctx, tx, current, generation, wire)
	if err != nil {
		return nil, err
	}
	return ref, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}

func validateWire(i Intent, wire Wire) error {
	if i.Phase == "document" {
		if len(wire.Body) == 0 || len(wire.Body) > telegram.MaxDocumentBytes {
			return ErrWireUnavailable
		}
		return nil
	}
	encoded, err := json.Marshal(wire)
	if err != nil || len(encoded) > maxRequestPayloadBytes || len(wire.Body) != 0 {
		return ErrWireUnavailable
	}
	prepared, err := telegram.PrepareSend(wire.Payload)
	if err != nil || !reflect.DeepEqual(prepared, wire.Payload) {
		return ErrWireUnavailable
	}
	return nil
}

func wireGeneration(ctx context.Context, tx pgx.Tx, owner string) (int64, error) {
	if owner == "" {
		return 0, nil
	}
	generation, err := fence.CurrentGeneration(ctx, tx, owner)
	if err != nil {
		return 0, err
	}
	return generation, fence.LockGeneration(ctx, tx, owner, &generation)
}

func stageWireSnapshot(ctx context.Context, tx pgx.Tx, i Intent, generation int64, wire Wire) (*WireReference, error) {
	key, err := wireKey(i)
	if err != nil {
		return nil, err
	}
	hash, err := wireDigest(wire)
	if err != nil {
		return nil, err
	}
	ref := &WireReference{Key: key + ":" + strconv.FormatInt(generation, 10) + ":candidate", Hash: hash}
	saved := wireSnapshot{Binding: wireIdentity(i), Generation: generation, ObservedAttempt: i.Attempt, Wire: wire}
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,0,$2,$3)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=$3`, i.Owner, ref.Key, saved)
	return ref, core.DatabaseOperationContextError(ctx, err)
}

func wireKey(i Intent) (string, error) {
	key, err := wireDigest([]any{i.BotID, i.Operation, i.Effect})
	return "delivery_wire:" + key, err
}

func captureGeneration(i Intent, ref WireReference) (int64, error) {
	key, err := wireKey(i)
	if err != nil || !strings.HasPrefix(ref.Key, key+":") {
		return 0, ErrWireUnavailable
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(ref.Key, key+":"), ":candidate")
	generation, err := strconv.ParseInt(encoded, 10, 64)
	if err != nil || generation < 0 || strconv.FormatInt(generation, 10) != encoded {
		return 0, ErrWireUnavailable
	}
	return generation, nil
}

// Acquire the private history fence before the caller's owner/shared locks.
func lockWireGeneration(ctx context.Context, tx pgx.Tx, i Intent, ref *WireReference) error {
	if ref == nil {
		return nil
	}
	saved, err := readWireSnapshot(ctx, tx, i, *ref)
	if err != nil || i.Owner == "" {
		return err
	}
	return fence.LockGeneration(ctx, tx, i.Owner, &saved.Generation)
}

func bindWireAttempt(ctx context.Context, tx pgx.Tx, i Intent, ref *WireReference) error {
	previous, chain, err := wireCaptureState(ctx, tx, i)
	if err != nil {
		return err
	}
	if chain && previous != nil && !reflect.DeepEqual(previous, ref) {
		return ErrWireUnavailable
	}
	if ref == nil {
		return nil
	}
	saved, err := readWireSnapshot(ctx, tx, i, *ref)
	if err != nil {
		return err
	}
	if saved.ObservedAttempt != i.Attempt-1 && saved.AdmittedAttempt != i.Attempt-1 {
		return ErrWireUnavailable
	}
	saved.AdmittedAttempt = i.Attempt
	saved.Phase, saved.Target = i.Phase, i.Target
	saved.Wire.Payload.ChatID = i.Chat
	saved.Wire.Payload.MessageID = 0
	if i.Phase == phaseEdit {
		saved.Wire.Payload.MessageID = i.Target
	}
	hash, err := wireDigest(saved.Wire)
	if err != nil {
		return err
	}
	key := strings.TrimSuffix(ref.Key, ":candidate")
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,0,$2,$3)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=$3`, i.Owner, key, saved)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	if strings.HasSuffix(ref.Key, ":candidate") {
		if _, err = tx.Exec(ctx, `UPDATE bot.interactions SET content=jsonb_build_object('generation',$3::bigint)
 WHERE owner=$1 AND update_id=0 AND kind=$2`, i.Owner, ref.Key, saved.Generation); err != nil {
			return core.DatabaseOperationContextError(ctx, err)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET wire_capture_key=$4,wire_capture_hash=$5
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`, i.BotID, i.Operation, i.Effect, key, hash)
	return core.DatabaseOperationContextError(ctx, err)
}

// AdmittedWire reads only the exact committed attempt. Missing private bytes
// never cause reconstruction after admission or after an uncertainty marker.
func AdmittedWire(ctx context.Context, db dbgen.DBTX, i Intent) (Wire, error) {
	ref, _, err := wireCaptureState(ctx, db, i)
	if err != nil {
		return Wire{}, err
	}
	if ref == nil {
		return Wire{}, ErrWireUnavailable
	}
	saved, err := readWireSnapshot(ctx, db, i, *ref)
	if err != nil {
		return Wire{}, err
	}
	if saved.AdmittedAttempt != i.Attempt || saved.Phase != i.Phase || saved.Target != i.Target {
		return Wire{}, ErrWireUnavailable
	}
	return saved.Wire, nil
}
