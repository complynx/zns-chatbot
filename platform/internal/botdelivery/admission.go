package botdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) Enqueue(ctx context.Context, in EnqueueRequest) (Observation, error) {
	owner, chat, operation, effect, reference, phase := in.Owner, in.Chat, in.Operation, in.Effect, in.Reference, in.Phase

	if err := s.Delivery.Validate(); err != nil {
		return Observation{}, err
	}
	if reference.Family == PassReceiptRedactionFamily || !reference.Valid(owner) || chat <= 0 ||
		(phase != phaseSend && phase != "document") {
		return Observation{}, ErrBinding
	}
	i := Intent{
		BotID:     s.Delivery.BotID,
		Operation: operation,
		Effect:    effect,
		Owner:     owner,
		Chat:      chat,
		Reference: reference,
		Phase:     phase,
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Observation{}, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.lockSource(ctx, tx, i); err != nil {
		return Observation{}, err
	}
	raw, err := json.Marshal(reference)
	if err != nil {
		return Observation{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.delivery_intents(bot_id,operation_key,effect_key,owner,chat_id,reference,phase,target_message_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		i.BotID,
		operation,
		effect,
		owner,
		chat,
		raw,
		phase,
		i.Target,
	)
	if err != nil {
		return Observation{}, core.DatabaseOperationContextError(ctx, err)
	}
	current, err := Read(ctx, tx, i.BotID, i.QueueReference(), true)
	if err != nil {
		return Observation{}, err
	}
	if current.Owner != owner || current.Chat != chat || !reflect.DeepEqual(current.Reference, reference) {
		return Observation{}, ErrBinding
	}
	_, err = delivery.Register(
		ctx,
		tx,
		i.BotID,
		i.QueueReference(),
		delivery.Destination{Chat: strconv.FormatInt(chat, 10)},
		delivery.Interactive,
	)
	if err != nil {
		return Observation{}, err
	}
	return current.Observation(), core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}
func (s Service) lockSource(ctx context.Context, tx pgx.Tx, i Intent) error {
	return s.lockRenderedSource(ctx, tx, i, nil)
}

func (s Service) lockRenderedSource(
	ctx context.Context,
	tx pgx.Tx,
	i Intent,
	exportEvents []string,
	renderedPass ...*PassCardReceipt,
) error {
	return s.lockPayloadSources(ctx, tx, i, exportEvents, nil, renderedPass...)
}

func (s Service) lockPayloadSources(
	ctx context.Context,
	tx pgx.Tx,
	i Intent,
	exportEvents []string,
	retained *Intent,
	renderedPass ...*PassCardReceipt,
) error {
	if !i.Reference.Valid(i.Owner) {
		return ErrBinding
	}
	if i.Reference.Kind == IdentityIntent {
		return nil
	}
	if i.Reference.Family == PassReceiptRedactionFamily {
		return s.lockPassReceiptRedaction(ctx, tx, i)
	}
	f, refs, binding, viewStale, err := s.payloadFamily(ctx, tx, i, exportEvents, renderedPass...)
	if err != nil {
		return err
	}
	lockRefs := refs
	if retained != nil {
		lockRefs, err = passPreparationLocks(refs, *retained)
		if err != nil {
			return err
		}
		if retained.Receipt.Pass != nil && retained.Receipt.Pass.Event != "" {
			f.events = append(f.events, retained.Receipt.Pass.Event)
		}
	}
	expanded, err := readsource.ExpandProposalSources(ctx, tx, lockRefs)
	if err != nil {
		return err
	}
	if err = readsource.LockRegistrationMutationEvents(ctx, tx, expanded, f.events); err != nil {
		return err
	}
	if err = s.lockFamilyEvent(ctx, tx, i); err != nil {
		return familyMissing(err)
	}
	if err = s.lockPayloadActors(ctx, tx, i, expanded, &f); err != nil {
		return err
	}
	if err = s.lockPayloadCapability(ctx, tx, i, binding); err != nil {
		return err
	}
	if !viewStale && (i.Reference.Family != familyPasses || binding == nil) {
		if err = s.lockFamily(ctx, tx, i, f); err != nil {
			return familyMissing(err)
		}
	}
	return s.validatePayloadSource(ctx, tx, i, refs, f, viewStale)
}

func refundDeliveryActor(ctx context.Context, tx pgx.Tx, i Intent) (string, error) {
	if i.Reference.Family != familyRefund {
		return "", nil
	}
	if i.Reference.Refund == nil {
		return "", ErrBinding
	}
	return orders.RefundDeliveryActorInTx(ctx, tx, i.Owner, i.Reference.Event, *i.Reference.Refund)
}

func (s Service) begin(
	ctx context.Context,
	observed Intent,
	target int64,
	exportEvents []string,
	pass *PassCardReceipt,
) (Intent, bool, error) {
	stored, err := s.readBeginObservation(ctx, observed, exportEvents)
	if err != nil {
		return observed, false, err
	}
	if stored.Attempt != observed.Attempt {
		return stored, false, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return observed, false, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if observed.Reference.Family == familyPasses && observed.Reference.Source != nil &&
		(pass == nil || pass.PreviousMessageID != target) {
		return observed, false, ErrBinding
	}
	previous, err := s.previousPassReceipt(ctx, tx, stored)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return observed, false, err
	}
	if err = s.lockPayloadSources(ctx, tx, stored, exportEvents, previous, pass); err != nil {
		return s.failPassAdmission(ctx, tx, stored, previous, err)
	}
	if observed.Reference.Kind == CardIntent && observed.Attempt == 0 &&
		observed.Reference.Family != PassReceiptRedactionFamily {
		if err = lockRenderedTarget(ctx, tx, observed, target); err != nil {
			return observed, false, err
		}
	}
	current, err := Read(ctx, tx, observed.BotID, observed.QueueReference(), true)
	if err != nil {
		return observed, false, err
	}
	if !sameBinding(current, observed) {
		return observed, false, ErrBinding
	}
	if current.State != delivery.Deferred || current.Attempt != observed.Attempt {
		return current, false, nil
	}
	current = bindAdmittedPass(current, pass)
	return s.beginAttempt(ctx, tx, current, target)
}

func (s Service) beginAttempt(ctx context.Context, tx pgx.Tx, current Intent, target int64) (Intent, bool, error) {
	var err error
	if current.Reference.Kind == CardIntent {
		var pendingReceipt bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.delivery_intents
   WHERE bot_id=$1 AND owner=$2 AND state='sent' AND NOT continuation_done
   AND reference->>'kind'='card' AND reference->>'card_key'=$3)`, current.BotID, current.Owner, current.Reference.CardKey).Scan(&pendingReceipt)
		if err != nil {
			return current, false, core.DatabaseOperationContextError(ctx, err)
		}
		if pendingReceipt {
			return current, false, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
		}
	}
	admission, err := delivery.Begin(ctx, tx, s.Delivery, current.QueueReference())
	if err != nil {
		return current, false, err
	}
	if !admission.Ready {
		return current, false, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
	}
	current.Attempt++
	current.State = delivery.Sending
	// A missing edit target is a persistent phase transition. Do not infer edit
	// again from an old view receipt after a definite fallback was admitted.
	if current.Attempt == 1 && current.Reference.Kind == CardIntent && target > 0 {
		current.Phase = phaseEdit
		current.Target = target
	}
	receipt, err := json.Marshal(current.Receipt)
	if err != nil {
		return current, false, err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.delivery_intents SET state='sending',attempt=$4,attempted_at=clock_timestamp(),phase=$5,target_message_id=$6,receipt=$7
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID,
		current.Operation,
		current.Effect,
		current.Attempt,
		current.Phase,
		current.Target,
		receipt,
	)
	if err != nil {
		return current, false, core.DatabaseOperationContextError(ctx, err)
	}
	return current, true, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}
func (s Service) Begin(ctx context.Context, in BeginRequest) (BeginResult, error) {
	if in.PreparationFailure {
		return s.failPassPreparation(ctx, in.Observed)
	}
	i, ready, err := s.begin(ctx, in.Observed, in.Target, in.ExportEvents, in.Pass)
	if err != nil {
		return BeginResult{}, err
	}
	return BeginResult{Intent: i, Ready: ready}, nil
}
func sameBinding(a, b Intent) bool {
	return a.BotID == b.BotID && a.Operation == b.Operation && a.Effect == b.Effect && a.Owner == b.Owner &&
		a.Chat == b.Chat &&
		reflect.DeepEqual(a.Reference, b.Reference)
}
func lockDerivation(ctx context.Context, tx pgx.Tx, owner string, source readsource.Derivation) error {
	if !source.Valid() {
		return ErrBinding
	}
	valid, err := readsource.Lock(ctx, tx, owner, source.Authorities)
	if err != nil {
		return err
	}
	if slices.Contains(valid, false) {
		return &core.ProblemError{Status: http.StatusConflict, Code: codePassSourceStale}
	}
	return fence.LockGeneration(ctx, tx, owner, source.Generation)
}

// Mutable projection staleness is decided only after the payload authority.
func (s Service) payloadFamily(ctx context.Context, tx pgx.Tx, i Intent, exportEvents []string,
	renderedPass ...*PassCardReceipt,
) (familyRead, []readsource.Authority, *PassCardReceipt, bool, error) {
	f, refs, err := s.prepareFamily(ctx, tx, i, exportEvents)
	viewStale := i.Reference.Family == familyPasses &&
		(i.Reference.Source != nil || validPassReceipt(i)) && errors.Is(err, ErrStale)
	if err != nil && !viewStale {
		return f, refs, nil, viewStale, familyMissing(err)
	}
	binding := i.Receipt.Pass
	if binding == nil && validPassReceipt(i) {
		// Old successful cards retain authority in their immutable Reference.
		// They never persisted a rendered capability. Do not adopt the new view.
		binding = &PassCardReceipt{PreviousMessageID: passPreviousTarget(i)}
	}
	if len(renderedPass) != 0 {
		// The payload authority survives a replaced or missing mutable projection.
		binding = renderedPass[0]
		if i.Reference.Family == familyPasses && binding != nil && !viewStale {
			expected := PassCardBinding(f.menu.RegistrationMenu, binding.PreviousMessageID)
			if !reflect.DeepEqual(binding, expected) {
				viewStale = true
			}
		}
	}
	if i.Reference.Family == familyPasses && binding != nil && binding.Event != "" {
		f.events = append(f.events, binding.Event)
	}
	return f, refs, binding, viewStale, nil
}

func (s Service) validatePayloadSource(
	ctx context.Context,
	tx pgx.Tx,
	i Intent,
	refs []readsource.Authority,
	f familyRead,
	viewStale bool,
) error {
	valid, err := readsource.Lock(ctx, tx, i.Owner, refs)
	if err != nil {
		return err
	}
	if slices.Contains(valid, false) {
		return passMenuDenied(i, &core.ProblemError{Status: http.StatusConflict, Code: codePassSourceStale})
	}
	if i.Reference.Source != nil {
		if err = fence.LockGeneration(ctx, tx, i.Owner, i.Reference.Source.Generation); err != nil {
			return passMenuDenied(i, err)
		}
	}
	if err = fence.LockGeneration(ctx, tx, i.Owner, i.Reference.Generation); err != nil {
		return passMenuDenied(i, err)
	}
	if viewStale || (i.Reference.Family == familyPasses && f.menu.Redacted) {
		return ErrStale
	}
	return lockViewBinding(ctx, tx, i, f)
}

func (s Service) lockPayloadActors(
	ctx context.Context,
	tx pgx.Tx,
	i Intent,
	expanded []readsource.Authority,
	f *familyRead,
) error {
	var err error
	actors := []string{i.Owner}
	if i.Reference.Family == familyPassProof {
		actors = append(actors, i.Reference.Object)
	}
	f.refundAmbassador, err = refundDeliveryActor(ctx, tx, i)
	if err != nil {
		return familyMissing(err)
	}
	if f.refundAmbassador != "" {
		actors = append(actors, f.refundAmbassador)
	}
	if err = readsource.LockActors(ctx, tx, actors, expanded); err != nil {
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
func (s Service) readBeginObservation(ctx context.Context, observed Intent, exportEvents []string) (Intent, error) {
	if err := s.Delivery.Validate(); err != nil {
		return Intent{}, err
	}
	if observed.BotID != s.Delivery.BotID || observed.QueueReference().Owner != delivery.Bot {
		return Intent{}, ErrBinding
	}
	if observed.Reference.Family == familyPassExport && !passbooking.ValidExportEvents(exportEvents) {
		return Intent{}, ErrBinding
	}
	stored, err := Read(ctx, s.DB, observed.BotID, observed.QueueReference(), false)
	if err != nil {
		return Intent{}, err
	}
	if !sameBinding(stored, observed) {
		return Intent{}, ErrBinding
	}

	return stored, nil
}

func (s Service) lockPayloadCapability(ctx context.Context, tx pgx.Tx, i Intent, binding *PassCardReceipt) error {
	var err error
	if i.Reference.Family == familyPasses && binding != nil && binding.Capability != "" {
		if err = lockRegistrationCapability(ctx, tx, i.Owner, binding.Event, binding.Capability); err != nil {
			return passMenuDenied(i, err)
		}
	}
	return nil
}

func (s Service) failPassAdmission(
	ctx context.Context,
	tx pgx.Tx,
	stored Intent,
	previous *Intent,
	cause error,
) (Intent, bool, error) {
	if _, denied := errors.AsType[*PassMenuDeniedError](cause); !denied || previous == nil {
		return stored, false, cause
	}
	retired, handled, err := s.retirePassPreparation(ctx, tx, stored, *previous)
	if handled || err != nil {
		return retired, false, err
	}
	return stored, false, cause
}

func bindAdmittedPass(current Intent, pass *PassCardReceipt) Intent {
	if current.Reference.Family == familyPasses && pass != nil {
		canonical := *pass
		if current.Attempt > 0 && current.Receipt.Pass != nil {
			canonical.PreviousMessageID = current.Receipt.Pass.PreviousMessageID
		}
		current.Receipt.Pass = &canonical
	}
	return current
}
