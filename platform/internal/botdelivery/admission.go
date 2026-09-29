package botdelivery

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) Enqueue(ctx context.Context, in EnqueueRequest) (Observation, error) {
	owner, chat, operation, effect, reference, phase := in.Owner, in.Chat, in.Operation, in.Effect, in.Reference, in.Phase

	if err := s.Delivery.Validate(); err != nil {
		return Observation{}, err
	}
	if !reference.Valid(owner) || chat <= 0 ||
		(phase != "send" && phase != "document") {
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
		return Observation{}, err
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
		return Observation{}, err
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
	return current.Observation(), tx.Commit(ctx)
}
func (s Service) lockSource(ctx context.Context, tx pgx.Tx, i Intent) error {
	return s.lockRenderedSource(ctx, tx, i, nil)
}
func (s Service) lockRenderedSource(ctx context.Context, tx pgx.Tx, i Intent, exportEvents []string) error {
	if !i.Reference.Valid(i.Owner) {
		return ErrBinding
	}
	if i.Reference.Kind == IdentityIntent {
		return nil
	}
	f, refs, err := s.prepareFamily(ctx, tx, i, exportEvents)
	if err != nil {
		return familyMissing(err)
	}
	expanded, err := readsource.ExpandProposalSources(ctx, tx, refs)
	if err != nil {
		return err
	}
	if err = readsource.LockRegistrationMutationEvents(ctx, tx, expanded, f.events); err != nil {
		return err
	}
	if err = s.lockFamilyEvent(ctx, tx, i); err != nil {
		return familyMissing(err)
	}
	actors := []string{i.Owner}
	if i.Reference.Family == "pass_proof" {
		actors = append(actors, i.Reference.Object)
	}
	if err = readsource.LockActors(ctx, tx, actors, expanded); err != nil {
		return err
	}
	var chat int64
	if err = tx.QueryRow(ctx, `SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE`, i.Owner).
		Scan(&chat); err != nil {
		return err
	}
	if chat != i.Chat {
		return ErrStale
	}
	if err = s.lockFamily(ctx, tx, i, f); err != nil {
		return familyMissing(err)
	}
	valid, err := readsource.Lock(ctx, tx, i.Owner, refs)
	if err != nil {
		return err
	}
	if slices.Contains(valid, false) {
		return &core.ProblemError{Status: http.StatusConflict, Code: "pass_source_stale"}
	}
	if i.Reference.Source != nil {
		if err = fence.LockGeneration(ctx, tx, i.Owner, i.Reference.Source.Generation); err != nil {
			return err
		}
	}
	if err = fence.LockGeneration(ctx, tx, i.Owner, i.Reference.Generation); err != nil {
		return err
	}
	return lockViewBinding(ctx, tx, i, f)
}

func (s Service) begin(
	ctx context.Context,
	observed Intent,
	target int64,
	exportEvents []string,
) (Intent, bool, error) {

	if err := s.Delivery.Validate(); err != nil {
		return observed, false, err
	}
	if observed.BotID != s.Delivery.BotID || observed.QueueReference().Owner != delivery.Bot {
		return observed, false, ErrBinding
	}
	if observed.Reference.Family == "pass_export" && !passbooking.ValidExportEvents(exportEvents) {
		return observed, false, ErrBinding
	}
	stored, err := Read(ctx, s.DB, observed.BotID, observed.QueueReference(), false)
	if err != nil {
		return observed, false, err
	}
	if !sameBinding(stored, observed) {
		return observed, false, ErrBinding
	}
	if stored.Attempt != observed.Attempt {
		return stored, false, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return observed, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.lockRenderedSource(ctx, tx, observed, exportEvents); err != nil {
		return observed, false, err
	}
	if observed.Reference.Kind == CardIntent && observed.Attempt == 0 {
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
	if current.Reference.Kind == CardIntent {
		var pendingReceipt bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.delivery_intents
   WHERE bot_id=$1 AND owner=$2 AND state='sent' AND NOT continuation_done
   AND reference->>'kind'='card' AND reference->>'card_key'=$3)`, current.BotID, current.Owner, current.Reference.CardKey).Scan(&pendingReceipt)
		if err != nil {
			return current, false, err
		}
		if pendingReceipt {
			return current, false, tx.Commit(ctx)
		}
	}
	admission, err := delivery.Begin(ctx, tx, s.Delivery, current.QueueReference())
	if err != nil {
		return current, false, err
	}
	if !admission.Ready {
		return current, false, tx.Commit(ctx)
	}
	current.Attempt++
	current.State = delivery.Sending
	// A missing edit target is a persistent phase transition. Do not infer edit
	// again from an old view receipt after a definite fallback was admitted.
	if current.Attempt == 1 && current.Reference.Kind == CardIntent && target > 0 {
		current.Phase = "edit"
		current.Target = target
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.delivery_intents SET state='sending',attempt=$4,attempted_at=clock_timestamp(),phase=$5,target_message_id=$6
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID,
		current.Operation,
		current.Effect,
		current.Attempt,
		current.Phase,
		current.Target,
	)
	if err != nil {
		return current, false, err
	}
	return current, true, tx.Commit(ctx)
}
func (s Service) Begin(ctx context.Context, in BeginRequest) (BeginResult, error) {
	i, ready, err := s.begin(ctx, in.Observed, in.Target, in.ExportEvents)
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
		return &core.ProblemError{Status: http.StatusConflict, Code: "pass_source_stale"}
	}
	return fence.LockGeneration(ctx, tx, owner, source.Generation)
}
