package agenthost

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// PlanSources supplies live domain decisions at each provider exposure.
type PlanSources interface {
	Generation(context.Context, string) (int64, error)
	MemoryState(context.Context, string) (knowledge.MemoryDeletionState, error)
	SourcesChanged(context.Context, string, []readsource.Authority) (bool, error)
	RegistrationContextChanged(context.Context, string, interaction.PassContextDependency) (bool, error)
}

// PlanCapture retains identities from all provider exposures, never private payloads.
type PlanCapture struct {
	Builder     ContextBuilder
	Sources     PlanSources
	Unavailable error
	mu          sync.Mutex
	authority   interaction.PlanAuthority
}

func (capture *PlanCapture) Reset() {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.authority = interaction.PlanAuthority{
		Reads:           []interaction.PassContextDependency{},
		ReadAuthorities: []readsource.Authority{},
	}
}

func (capture *PlanCapture) Expose(ctx context.Context, owner string, input *agent.Input) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	state, err := capture.Sources.MemoryState(ctx, owner)
	if err != nil {
		return fmt.Errorf("%w: %w", capture.Unavailable, err)
	}
	if err = capture.Builder.Refresh(ctx, AdmittedInput{Owner: owner, Projection: input}); err != nil {
		return fmt.Errorf("%w: %w", capture.Unavailable, err)
	}
	if input.Knowledge != nil {
		input.Knowledge.ReadState = &state
	}
	if capture.authority.Reads == nil {
		capture.authority.Reads = []interaction.PassContextDependency{}
	}
	for _, dependency := range ScriptPassContext(input.Registration) {
		known := false
		for _, previous := range capture.authority.Reads {
			if samePassDependency(previous, dependency) {
				known = true
				break
			}
		}
		if !known {
			capture.authority.Reads = append(capture.authority.Reads, dependency)
		}
	}
	if input.Script != nil {
		for _, run := range input.Script.Runs {
			if !run.PassRedacted {
				capture.authority.Scripts = true
			}
		}
	}
	capture.authority.PrivateHistory = capture.authority.PrivateHistory || InputPrivateHistory(input)
	return capture.addReadAuthorities(input)
}

func (capture *PlanCapture) Snapshot() *interaction.PlanAuthority {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return &interaction.PlanAuthority{
		PrivateHistory:  capture.authority.PrivateHistory,
		ReadAuthorities: readsource.CloneAuthorities(capture.authority.ReadAuthorities),
		Reads:           append([]interaction.PassContextDependency{}, capture.authority.Reads...),
		Scripts:         capture.authority.Scripts,
	}
}

func (capture *PlanCapture) addReadAuthorities(input *agent.Input) error {
	direct, err := PassContextReadAuthorities(ScriptPassContext(input.Registration))
	if err != nil {
		return err
	}
	history, err := HistoryInputReadAuthorities(input)
	if err != nil {
		return err
	}
	groups := [][]readsource.Authority{
		capture.authority.ReadAuthorities,
		direct,
		history,
		KnowledgeReadAuthorities(input.Knowledge),
	}
	if input.Script != nil {
		groups = append(groups, input.Script.ReadAuthorities)
	}
	capture.authority.ReadAuthorities, err = MergeReadAuthorities(groups...)
	if err != nil {
		return err
	}
	return RetainModelKnowledgeEvidence(input)
}

func samePassDependency(first, second interaction.PassContextDependency) bool {
	if first.Request != second.Request || !slices.Equal(first.QueueAuthorities, second.QueueAuthorities) ||
		!slices.Equal(first.Invitations, second.Invitations) ||
		!samePassIdentity(first.TargetBooking, second.TargetBooking) {
		return false
	}
	if first.Booking == nil || second.Booking == nil {
		return first.Booking == nil && second.Booking == nil
	}
	return *first.Booking == *second.Booking
}

func samePassIdentity(first, second *interaction.BookingIdentity) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

// PlanAuthorization rechecks saved model evidence through current domain decisions.
type PlanAuthorization struct {
	DB              *pgxpool.Pool
	Terminal        error
	HistoryTerminal error
	Unavailable     error
	Sources         PlanSources
	Scripts         ScriptStore
}

func (policy PlanAuthorization) Changed(
	ctx context.Context,
	owner string,
	updateID int64,
	authority *interaction.PlanAuthority,
) (bool, error) {
	if authority == nil || authority.Reads == nil || authority.ReadAuthorities == nil {
		return true, nil
	}
	if handled, changed, err := policy.inspectPlanChanged(ctx, owner, updateID, authority); handled {
		return changed, err
	}
	return policy.changed(ctx, owner, updateID, authority)
}

func (policy PlanAuthorization) changed(ctx context.Context, owner string, updateID int64,
	authority *interaction.PlanAuthority) (bool, error) {
	if changed, err := policy.Sources.SourcesChanged(ctx, owner, authority.ReadAuthorities); err != nil || changed {
		return changed, err
	}
	for _, dependency := range authority.Reads {
		changed, err := policy.Sources.RegistrationContextChanged(ctx, owner, dependency)
		if err != nil || changed {
			return changed, err
		}
	}
	if !authority.Scripts {
		return false, nil
	}
	records, err := policy.Scripts.LoadAuthorized(ctx, owner, updateID)
	if err != nil {
		return false, err
	}
	if len(records) == 0 {
		return true, nil
	}
	for _, record := range records {
		if record.PassRedacted {
			return true, nil
		}
	}
	return false, nil
}

// Collect the exact ledger revision before its one current authority decision.
// Unsupported carriers retain the ordinary independent validation path.
func (policy PlanAuthorization) inspectPlanChanged(ctx context.Context, owner string, updateID int64,
	authority *interaction.PlanAuthority) (bool, bool, error) {
	scriptPolicy, standard := policy.Scripts.Policy.(ScriptAuthorization)
	if !standard || !authority.Scripts || len(authority.Reads) != 0 ||
		!reflect.ValueOf(policy.Sources).Comparable() || policy.Sources != scriptPolicy.ScriptDomainAuthority ||
		!readsource.Valid(authority.ReadAuthorities) {
		return false, false, nil
	}
	for range scriptLedgerAttempts {
		snapshot, err := policy.Scripts.ledgerSnapshot(ctx, owner, updateID)
		if err != nil {
			// A failed speculative collection retains the complete ordinary path.
			changed, fallbackErr := policy.changed(ctx, owner, updateID, authority)
			return true, changed, fallbackErr
		}
		if !inspectPlanCarriersCovered(owner, snapshot.records, authority.ReadAuthorities) {
			return false, false, nil
		}
		if changed, sourceErr := policy.Sources.SourcesChanged(
			ctx,
			owner,
			authority.ReadAuthorities,
		); changed ||
			sourceErr != nil {
			return true, changed, sourceErr
		}
		unchanged, err := policy.Scripts.commitLedger(ctx, owner, updateID, snapshot, false, nil)
		if err != nil || unchanged {
			return true, false, err
		}
	}
	return true, false, ErrScriptLedgerConflict
}

func inspectPlanCarriersCovered(owner string, records []ScriptRecord, checked []readsource.Authority) bool {
	if len(records) == 0 {
		return false
	}
	for _, record := range records {
		if scriptRetired(record) || record.PrivateProfile || record.PassContext == nil ||
			len(record.PassContext) != 0 || record.ReadAuthorities == nil || len(record.Calls) == 0 {
			return false
		}
		for _, call := range record.Calls {
			if call.Outcome.Name != modernOrdersInspect || call.Source == nil {
				return false
			}
		}
		refs, batched, err := scriptRecordAuthorityChecks(owner, record)
		if err != nil || !batched {
			return false
		}
		for _, ref := range refs {
			if !slices.ContainsFunc(
				checked,
				func(other readsource.Authority) bool { return readsource.Equal(ref, other) },
			) {
				return false
			}
		}
	}
	return true
}

func (policy PlanAuthorization) ValidatePlan(
	ctx context.Context,
	owner string,
	updateID int64,
	plan interaction.SavedPlan,
) error {
	if plan.SystemNotice != "" && plan.TerminalReason != interaction.SourceRevoked {
		return nil
	}
	if plan.TerminalReason != interaction.SourceRevoked && !passPlanUsesModelText(plan) {
		var completed bool
		if err := policy.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND ((kind='registration_action' AND content->>'committed'='true') OR (kind='reply_origin' AND content='"authoritative"'::jsonb)))`, owner, updateID).
			Scan(&completed); err != nil {
			return core.DatabaseOperationContextError(ctx, err)
		}
		if completed {
			return nil
		}
	}
	return policy.ValidateAuthority(ctx, owner, updateID, plan)
}

func (policy PlanAuthorization) ValidateAuthority(
	ctx context.Context,
	owner string,
	updateID int64,
	plan interaction.SavedPlan,
) error {
	changed := (plan.TerminalReason == interaction.SourceRevoked)
	if !changed {
		var err error
		changed, err = policy.Changed(ctx, owner, updateID, plan.PassAuthority)
		if err != nil {
			return fmt.Errorf("%w: %w", policy.Unavailable, err)
		}
	}
	if !changed {
		return nil
	}
	return policy.terminal(ctx, owner, updateID, plan.HistoryGeneration)
}

func (policy PlanAuthorization) terminal(ctx context.Context, owner string, updateID, generation int64) error {
	if err := (interaction.Store{DB: policy.DB}).MarkTerminal(
		ctx,
		owner,
		updateID,
		generation,
		interaction.SourceRevoked,
	); err != nil {
		return err
	}
	return policy.Terminal
}

func (policy PlanAuthorization) ValidateAgentPlan(
	ctx context.Context,
	owner string,
	updateID int64,
	plan interaction.SavedPlan,
) error {
	if err := policy.ValidateHistoryPlan(ctx, owner, updateID, plan); err != nil {
		return err
	}
	return policy.ValidatePlan(ctx, owner, updateID, plan)
}

func (policy PlanAuthorization) ValidateReply(ctx context.Context, owner string, updateID int64) error {
	plan, err := (interaction.Store{DB: policy.DB}).Load(ctx, owner, updateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy.validateMissingReply(ctx, owner, updateID)
	}
	if err != nil {
		return err
	}
	return policy.ValidateDerivedReply(ctx, owner, updateID, plan)
}

func (policy PlanAuthorization) ReplyVisible(ctx context.Context, owner string, updateID int64) (bool, error) {
	err := policy.ValidateReply(ctx, owner, updateID)
	if errors.Is(err, policy.Terminal) || errors.Is(err, policy.HistoryTerminal) {
		return false, nil
	}
	return err == nil, err
}

func (policy PlanAuthorization) validateMissingReply(ctx context.Context, owner string, updateID int64) error {
	var modelReply bool
	err := policy.DB.QueryRow(ctx, `SELECT
 (EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2
 AND kind IN ('reply','orders_reply','knowledge_reply','profile_answer','registration_reply'))
 OR EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND kind='media_model_reply' AND content=to_jsonb($2::bigint)))
 AND NOT EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2
 AND kind='reply_origin' AND content='"authoritative"'::jsonb)`, owner, updateID).Scan(&modelReply)
	if err != nil || !modelReply {
		return core.DatabaseOperationContextError(ctx, err)
	}
	generation, err := policy.Sources.Generation(ctx, owner)
	if err != nil {
		return err
	}
	return policy.terminal(ctx, owner, updateID, generation)
}

func (policy PlanAuthorization) ValidateDerivedReply(
	ctx context.Context,
	owner string,
	updateID int64,
	plan interaction.SavedPlan,
) error {
	if err := policy.ValidateHistoryPlan(ctx, owner, updateID, plan); err != nil {
		return err
	}
	return policy.ValidateAuthority(ctx, owner, updateID, plan)
}

func (policy PlanAuthorization) ValidateHistoryPlan(
	ctx context.Context,
	owner string,
	updateID int64,
	plan interaction.SavedPlan,
) error {
	generation, err := policy.Sources.Generation(ctx, owner)
	if err != nil {
		return err
	}
	if plan.HistoryGeneration == generation && plan.TerminalReason != interaction.HistoryDeleted {
		return nil
	}
	if err = (interaction.Store{DB: policy.DB}).MarkTerminal(
		ctx,
		owner,
		updateID,
		generation,
		interaction.HistoryDeleted,
	); err != nil {
		return err
	}
	return policy.HistoryTerminal
}

func (policy PlanAuthorization) ValidateHistoryInteractions(
	ctx context.Context,
	owner string,
	updateID, generation int64,
) error {
	var stale bool
	err := policy.DB.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM bot.interactions i, jsonb_array_elements(i.content) r
 WHERE i.owner=$1 AND i.update_id=$2 AND i.kind IN ('history_reads','script_runs')
 AND (COALESCE((CASE WHEN i.kind='script_runs' THEN r->>'history_generation' ELSE r->>'generation' END)::bigint,0)<>$3
 OR r->>'history_redacted'='true' OR r->>'error'='history_deleted'))`, owner, updateID, generation).Scan(&stale)
	if err != nil || !stale {
		return core.DatabaseOperationContextError(ctx, err)
	}
	return policy.ValidateHistoryPlan(
		ctx,
		owner,
		updateID,
		interaction.SavedPlan{TerminalReason: interaction.HistoryDeleted},
	)
}

func passPlanUsesModelText(plan interaction.SavedPlan) bool {
	return plan.Kind == interaction.DerivedPlan
}
