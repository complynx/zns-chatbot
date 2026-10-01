package botdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// PassReceiptRedactionFamily is private to successful-receipt retirement.
// Public enqueue cannot create it. Its payload is always a fixed empty-keyboard edit.
const PassReceiptRedactionFamily = "pass_receipt_redaction"

// PassMenuDeniedCode preserves definitive denial across the HTTP host boundary.
const PassMenuDeniedCode = "pass_menu_denied"

// PassMenuDeniedError records an actual source/history/capability denial. An obsolete
// view or target binding does not produce this error.
type PassMenuDeniedError struct{ Cause error }

func (e *PassMenuDeniedError) Error() string { return e.Cause.Error() }
func (e *PassMenuDeniedError) Unwrap() error { return e.Cause }

func (e *PassMenuDeniedError) As(target any) bool {
	problem, ok := target.(**core.ProblemError)
	if ok {
		*problem = &core.ProblemError{Status: http.StatusConflict, Code: PassMenuDeniedCode}
	}
	return ok
}

func passMenuDenied(i Intent, cause error) error {
	if i.Reference.Family != familyPasses ||
		(i.Reference.Source == nil && !validPassReceipt(i)) || core.IsDatabaseFailure(cause) {
		return cause
	}
	problem, ok := errors.AsType[*core.ProblemError](cause)
	if errors.Is(cause, ErrStale) || (ok && (problem.Code == codePassSourceStale || problem.Code == "history_stale")) {
		return &PassMenuDeniedError{Cause: cause}
	}
	return cause
}

// retirePassReceipt commits callback retirement and one authorized actual target
// with continuation completion. No revoked payload is projected to get a target.
func (s Service) retirePassReceipt(
	ctx context.Context,
	tx pgx.Tx,
	i Intent,
	preserve []string,
	retiredPrevious int64,
) error {
	preserve = append([]string{}, preserve...)
	var previous int64
	tombstone := PassMenu{Source: i.Reference.Source, Redacted: true}
	err := tx.QueryRow(ctx, `UPDATE bot.pass_views SET state=$4,view_hash=''
 WHERE owner=$1 AND revision=$2 AND state->'source'=$3 AND chat_id=$5
 AND (message_id=$6 OR (message_id=$8 AND $8>0) OR (message_id=0 AND $7=0))
 RETURNING message_id`, i.Owner, i.Reference.Revision, i.Reference.Source, tombstone, i.Chat, i.MessageID, passPreviousTarget(i), retiredPrevious).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationContextError(ctx, err)
	}
	// A newer view cannot keep callbacks from the revoked original revision.
	if _, err = tx.Exec(
		ctx,
		`DELETE FROM bot.pass_buttons WHERE owner=$1 AND revision=$2 AND NOT(token=ANY($3))`,
		i.Owner,
		i.Reference.Revision,
		preserve,
	); err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	return s.enqueuePassReceiptRedaction(ctx, tx, i, i.MessageID)
}

func passReceiptRedactionReference(parent Intent, target int64) Reference {
	return Reference{
		Kind: CardIntent, Family: PassReceiptRedactionFamily, CardKey: familyPasses,
		Revision: parent.Reference.Revision, Object: parent.Operation,
		ResultKind: parent.Effect, Version: target,
	}
}

func (s Service) enqueuePassReceiptRedaction(ctx context.Context, tx pgx.Tx, parent Intent, target int64) error {
	ref := passReceiptRedactionReference(parent, target)
	raw, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(append(fmt.Appendf(nil, "%d:", parent.BotID), raw...))
	key := "pass_retirement:" + hex.EncodeToString(digest[:])
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.delivery_intents(bot_id,operation_key,effect_key,owner,chat_id,reference,phase,target_message_id)
 VALUES($1,$2,'view',$3,$4,$5,'edit',$6) ON CONFLICT DO NOTHING`,
		parent.BotID,
		key,
		parent.Owner,
		parent.Chat,
		raw,
		target,
	)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	_, err = delivery.Register(ctx, tx, parent.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: key, Effect: "view"},
		delivery.Destination{Chat: strconv.FormatInt(parent.Chat, 10)}, delivery.Interactive)
	return err
}

// Only a receipt-created immutable row can authorize this source-free notice.
// The original receipt supplies owner, chat, revision and retained source identity.
func (s Service) lockPassReceiptRedaction(ctx context.Context, tx pgx.Tx, i Intent, actual *Intent) error {
	if i.Phase != phaseEdit || i.Target <= 0 || i.Target != i.Reference.Version {
		return ErrBinding
	}
	stored, err := Read(ctx, tx, i.BotID, i.QueueReference(), false)
	if err != nil {
		return err
	}
	if !sameBinding(stored, i) || stored.Target != i.Target || stored.Phase != phaseEdit {
		return ErrBinding
	}
	parent, err := Read(ctx, tx, i.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: i.Reference.Object, Effect: i.Reference.ResultKind}, false)
	if err != nil {
		return err
	}
	if parent.State != delivery.Succeeded || !parent.ContinuationDone || parent.Owner != i.Owner ||
		parent.Chat != i.Chat ||
		!validPassReceipt(parent) ||
		!reflect.DeepEqual(i.Reference, passReceiptRedactionReference(parent, i.Target)) {
		return ErrBinding
	}
	if err = s.lockPassRetirementTarget(ctx, tx, i, parent, actual); err != nil {
		return err
	}
	var chat int64
	if err = tx.QueryRow(ctx, `SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE`, i.Owner).
		Scan(&chat); err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	if chat != i.Chat {
		return ErrStale
	}
	return nil
}

// A delayed cleanup owns a target only while its actual payload is denied.
// Discovery stays ahead of intent and delivery-lane locks.
func (s Service) lockPassRetirementTarget(
	ctx context.Context,
	tx pgx.Tx,
	cleanup, parent Intent,
	actual *Intent,
) error {
	if actual == nil {
		return ErrBinding
	}
	latest, err := s.latestPassReceipt(ctx, tx, parent, cleanup.Target)
	if err != nil {
		return err
	}
	if !sameReceipt(*latest, *actual) {
		return passReceiptChanged()
	}
	denied, err := s.passReceiptDenied(ctx, tx, *actual)
	if err != nil {
		return err
	}
	latest, err = s.latestPassReceipt(ctx, tx, parent, cleanup.Target)
	if err != nil {
		return err
	}
	if !sameReceipt(*latest, *actual) {
		return passReceiptChanged()
	}
	if !denied {
		return ErrStale
	}
	_, err = tx.Exec(ctx, `DELETE FROM bot.pass_buttons WHERE owner=$1 AND revision=$2 AND token=ANY($3)`,
		actual.Owner, actual.Receipt.Revision, actual.Receipt.Tokens)
	return core.DatabaseOperationContextError(ctx, err)
}

func passReceiptChanged() error {
	return &core.ProblemError{Status: http.StatusServiceUnavailable, Code: "pass_receipt_changed"}
}

// The lane lock excludes an earlier transport finish after this final check.
// Only compare the discovered receipt here; domain locks must remain earlier.
func (s Service) validatePassRetirementTarget(ctx context.Context, tx pgx.Tx, current Intent, actual *Intent) error {
	if current.Reference.Family != PassReceiptRedactionFamily {
		return nil
	}
	if actual == nil {
		return ErrBinding
	}
	latest, err := s.latestPassReceipt(ctx, tx, current, current.Target)
	if err != nil {
		return err
	}
	if !sameReceipt(*latest, *actual) {
		return passReceiptChanged()
	}
	return nil
}

// PassCardBinding describes the menu state that produced the actual payload.
// The host validates it under the same source/menu locks before admission.
func PassCardBinding(state interaction.RegistrationMenu, previous int64) *PassCardReceipt {
	binding := &PassCardReceipt{PreviousMessageID: previous, Event: state.Event}
	switch state.View {
	case "queue", "admin_target":
		binding.Capability = "admin_assign"
	case "payment_queue":
		binding.Capability = "proof_accept"
	case "takeover_target":
		binding.Capability = "takeover"
	}
	return binding
}

func passPreviousTarget(i Intent) int64 {
	if i.Receipt.Pass != nil {
		return i.Receipt.Pass.PreviousMessageID
	}
	if i.Phase == phaseEdit {
		return i.Target
	}
	return 0
}

// A mutable view cannot identify the old visible payload. Only an exact saved
// successful receipt can authorize preparation-time cleanup of that target.
func (s Service) previousPassReceipt(ctx context.Context, tx pgx.Tx, pending Intent) (*Intent, error) {
	if pending.Reference.Family == PassReceiptRedactionFamily {
		return s.latestPassReceipt(ctx, tx, pending, pending.Target)
	}
	target := passPreviousTarget(pending)
	if pending.Reference.Family != familyPasses || pending.Reference.Source == nil || target <= 0 {
		return nil, pgx.ErrNoRows
	}
	return s.latestPassReceipt(ctx, tx, pending, target)
}

func (s Service) latestPassReceipt(ctx context.Context, tx pgx.Tx, pending Intent, target int64) (*Intent, error) {
	var operation, effect string
	err := tx.QueryRow(ctx, `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE bot_id=$1 AND owner=$2 AND chat_id=$3 AND state='sent' AND message_id=$4
 AND reference->>'family'='passes'
 ORDER BY attempted_at DESC NULLS LAST,created_at DESC,operation_key DESC,effect_key DESC LIMIT 1`, pending.BotID, pending.Owner, pending.Chat, target).Scan(&operation, &effect)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	previous, err := Read(ctx, tx, pending.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}, false)
	if err != nil {
		return nil, err
	}
	if !validPassReceipt(previous) || previous.Owner != pending.Owner || previous.Chat != pending.Chat ||
		previous.MessageID != target {
		return nil, ErrBinding
	}
	return &previous, nil
}

// Recheck the actual prior payload under the existing domain locks. A denial of
// a newly rendered capability alone cannot retire a different authorized card.
func (s Service) retirePassPreparation(ctx context.Context, tx pgx.Tx, pending, previous Intent) (Intent, bool, error) {
	latest, err := s.previousPassReceipt(ctx, tx, pending)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return pending, false, err
	}
	if latest == nil || !sameReceipt(*latest, previous) {
		// Discovery raced another successful payload. Retry with its actual source.
		return pending, false, &core.ProblemError{Status: http.StatusServiceUnavailable, Code: "pass_receipt_changed"}
	}
	retire, err := s.passReceiptDenied(ctx, tx, previous)
	if err != nil {
		return pending, false, err
	}
	current, err := Read(ctx, tx, pending.BotID, pending.QueueReference(), true)
	if err != nil {
		return pending, false, err
	}
	if !sameBinding(current, pending) {
		return pending, false, ErrBinding
	}
	if current.State != delivery.Deferred || current.Attempt != pending.Attempt {
		return current, true, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
	}
	actual, err := Read(ctx, tx, previous.BotID, previous.QueueReference(), true)
	if err != nil {
		return pending, false, err
	}
	if !sameReceipt(actual, previous) {
		return pending, false, ErrBinding
	}
	// Cancel before adding edits to the same shared lane; no network call occurs.
	if err = delivery.Project(
		ctx,
		tx,
		current.BotID,
		current.QueueReference(),
		delivery.Cancelled,
		current.NotBefore,
	); err != nil {
		return pending, false, err
	}
	_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET state='cancelled',reason='source_unavailable'
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`, current.BotID, current.Operation, current.Effect)
	if err != nil {
		return pending, false, core.DatabaseOperationContextError(ctx, err)
	}
	if retire {
		if err = s.retirePassReceipt(ctx, tx, actual, nil, 0); err != nil {
			return pending, false, err
		}
		_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET continuation_done=true
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND state='sent' AND attempt=$4 AND message_id=$5`, actual.BotID, actual.Operation, actual.Effect, actual.Attempt, actual.MessageID)
		if err != nil {
			return pending, false, core.DatabaseOperationContextError(ctx, err)
		}
	}
	current.State = delivery.Cancelled
	return current, true, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}

// Combine only lock discovery. Current and prior payload validity remain
// independently checked, so one revoked payload cannot condemn the other.
func passPreparationLocks(current []readsource.Authority, prior Intent) ([]readsource.Authority, error) {
	refs, err := readsource.Merge(current, prior.Reference.Authorities)
	if err != nil {
		return nil, err
	}
	if prior.Reference.Source != nil {
		return readsource.Merge(refs, prior.Reference.Source.Authorities)
	}
	return refs, nil
}

func validPassReceipt(i Intent) bool {
	if i.State != delivery.Succeeded || i.MessageID <= 0 || i.BotID <= 0 || i.Chat <= 0 ||
		i.Reference.Kind != CardIntent || !i.Reference.Valid(i.Owner) ||
		i.Reference.Family != familyPasses || i.Reference.CardKey != familyPasses ||
		i.Attempt <= 0 || i.Receipt.Kind != passCardReceiptKind || i.Receipt.Revision != i.Reference.Revision {
		return false
	}
	binding := i.Receipt.Pass
	if binding == nil {
		return predecessorPassReceipt(i)
	}
	if binding.PreviousMessageID < 0 {
		return false
	}
	switch binding.Capability {
	case "":
		return true
	case "admin_assign", "proof_accept", "takeover":
		return binding.Event != ""
	default:
		return false
	}
}

// The predecessor producer retained source/history authority in Reference and
// canonical payload hashes/tokens in both continuations. Rendering could change
// their hashes, so equality is not required. No mutable menu supplies authority.
func predecessorPassReceipt(i Intent) bool {
	r := i.Reference.Continuation
	return i.Reference.Generation != nil && r.Pass == nil && r.Kind == passCardReceiptKind &&
		r.Revision == i.Reference.Revision && canonicalPassContinuation(r) && canonicalPassContinuation(i.Receipt)
}

func canonicalPassContinuation(r Continuation) bool {
	hash, err := hex.DecodeString(r.ViewHash)
	if err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != r.ViewHash {
		return false
	}
	for _, token := range r.Tokens {
		decoded, decodeErr := hex.DecodeString(token)
		if decodeErr != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != token {
			return false
		}
	}
	return true
}

// Preparation failure shares admission's canonical receipt and lock owner.
// The caller supplies neither a cleanup target nor a denial decision.
func (s Service) failPassPreparation(ctx context.Context, observed Intent) (BeginResult, error) {
	if err := s.Delivery.Validate(); err != nil {
		return BeginResult{}, err
	}
	if observed.BotID != s.Delivery.BotID || observed.Reference.Family != familyPasses ||
		observed.Reference.Source == nil {
		return BeginResult{}, ErrBinding
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return BeginResult{}, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := Read(ctx, tx, observed.BotID, observed.QueueReference(), false)
	if err != nil {
		return BeginResult{}, err
	}
	if !sameBinding(current, observed) {
		return BeginResult{}, ErrBinding
	}
	if current.State != delivery.Deferred || current.Attempt != observed.Attempt {
		return BeginResult{Intent: current}, nil
	}
	prior, err := s.previousPassReceipt(ctx, tx, current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return BeginResult{}, err
	}
	if err = s.lockPayloadSources(ctx, tx, current, nil, prior); err == nil {
		return BeginResult{Intent: current}, nil
	}
	if !sourceDenied(err) {
		return BeginResult{}, err
	}
	if prior == nil {
		return BeginResult{Intent: current}, nil
	}
	cancelled, _, err := s.retirePassPreparation(ctx, tx, current, *prior)
	return BeginResult{Intent: cancelled}, err
}

func (s Service) passReceiptDenied(ctx context.Context, tx pgx.Tx, previous Intent) (bool, error) {
	err := s.lockSource(ctx, tx, previous)
	if err == nil {
		return false, nil
	}
	_, denied := errors.AsType[*PassMenuDeniedError](err)
	if denied || sourceDenied(err) {
		return denied, nil
	}
	return false, err
}
