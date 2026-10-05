package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

// turnLeaves adapts domain and storage leaves to the admitted host. The host
// receives only the named ports; model orchestration is not implemented here.
type turnLeaves struct {
	id    int64
	bot   *Bot
	input incoming
}

func (b *Bot) turnHost(in incoming, id int64) *agenthost.Turn {
	leaves := turnLeaves{bot: b, input: in, id: id}
	return &agenthost.Turn{
		Input: agenthost.TurnInput{Owner: in.owner, UpdateID: id, Text: in.text, MediaID: in.mediaID,
			TrustedPartnerIDs: registrationContacts(in.assetMessage)},
		State:     leaves,
		Budget:    agenthost.QuestionQuota{DB: b.DB, Limit: b.AssistantDailyLimit, Preferences: b.API},
		Initial:   leaves,
		Context:   b.contextBuilder(),
		Assets:    b.assetSource(in),
		Model:     agenthost.Provider{Model: b.Model, SettingsReader: b.API, History: b.API},
		Reads:     leaves,
		History:   b.historyReader(),
		Knowledge: b.knowledgeReader(),
		AV:        leaves,
		Binding:   leaves,
		Exposure: turnExposure{
			bot:   b,
			owner: in.owner,
			id:    id,
			capture: &agenthost.PlanCapture{
				Builder:     b.contextBuilder(),
				Sources:     botScriptAuthority{bot: b},
				Unavailable: errPassAuthorityUnavailable,
			},
		},
	}
}

func (s turnLeaves) ValidateReply(ctx context.Context, owner string, id int64) error {
	return s.bot.planAuthorization().ValidateReply(ctx, owner, id)
}
func (s turnLeaves) ValidateHistory(ctx context.Context, owner string, id, generation int64) error {
	return s.bot.planAuthorization().ValidateHistoryInteractions(ctx, owner, id, generation)
}
func (s turnLeaves) ValidatePlan(ctx context.Context, owner string, id int64, plan interaction.SavedPlan) error {
	return s.bot.planAuthorization().ValidateAgentPlan(ctx, owner, id, plan)
}
func (s turnLeaves) ClearAV(ctx context.Context, owner string, ids []string) error {
	return s.bot.clearAVResults(ctx, owner, ids)
}
func (s turnLeaves) Current(ctx context.Context, owner string) (workflow.Workflow, error) {
	return s.bot.API.Current(ctx, owner)
}
func (s turnLeaves) Catalog(ctx context.Context, owner string) ([]workflow.Slot, error) {
	return s.bot.API.Catalog(ctx, owner)
}
func (s turnLeaves) CurrentAV(ctx context.Context, input *agent.Input) error {
	return s.bot.addCurrentAV(ctx, s.input, input)
}
func (s turnLeaves) Orders(ctx context.Context, owner string, input *agent.Input) error {
	return s.bot.addOrderContext(ctx, owner, input)
}
func (s turnLeaves) Profile(ctx context.Context, owner string, input *agent.Input) error {
	return s.bot.addProfileContext(ctx, owner, input)
}
func (s turnLeaves) Media(ctx context.Context, input *agent.Input) error {
	return s.bot.addMediaContext(ctx, s.input, input)
}
func (s turnLeaves) Lineup(p agent.LineupQuery, input *agent.Input) error {
	return s.bot.performLineupRead(p, input)
}

func (s turnLeaves) Registration(
	ctx context.Context,
	owner string,
	id int64,
	p agent.RegistrationProposal,
	input *agent.Input,
) error {
	return s.bot.performRegistrationRead(ctx, owner, id, p, input)
}

func (s turnLeaves) Script(
	ctx context.Context,
	owner string,
	id int64,
	p agent.ScriptProposal,
	input *agent.Input,
) error {
	return s.bot.scriptHost().Perform(ctx, owner, id, p, input)
}

func (s turnLeaves) Inspection(ctx context.Context, owner string, id int64) (*agent.AVInspectionContext, error) {
	return s.bot.avInspectionContext(ctx, owner, id)
}

func (s turnLeaves) Refine(
	ctx context.Context,
	owner string,
	id int64,
	p agent.MediaProposal,
	input *agent.Input,
) (i18n.ID, error) {
	return s.bot.refineAV(ctx, owner, id, p, input)
}

func (s turnLeaves) Commands(
	ctx context.Context,
	owner string,
	p agent.Plan,
	input, request agent.Input,
	cached *interaction.SavedPlan,
) error {
	return s.bot.bindPlanCommands(ctx, owner, s.id, p, input, request, cached)
}
func (s turnLeaves) MediaSelection(cached *interaction.SavedPlan, input, request agent.Input) {
	cacheMediaSelection(cached, s.input, input, request)
}

// Failure propagates database failures so the durable inbox row survives and
// Run stops, instead of saving an "agent unavailable" reply.
func (s turnLeaves) Failure(err error) (i18n.ID, bool) {
	// Record fixed labels only; underlying errors can contain private values.
	reason := "unknown"
	if errors.Is(err, readsource.ErrLimit) {
		reason = "source_authority_limit"
	} else if err != nil {
		switch err.Error() {
		case "model input exceeds budget", "model endpoint unavailable", "model transport unavailable",
			"model unavailable", "invalid model response", "fixture request scope missing", "invalid derivation":
			reason = err.Error()
		}
	}
	s.bot.logger().Warn("agent planning failure", "reason", reason)
	return paidFailureNotice(
			err,
		), errors.Is(err, errPassPlanTerminal) || errors.Is(err, interaction.ErrOrderReadUnavailable) ||
			errors.Is(err, errPassAuthorityUnavailable) || core.IsDatabaseFailure(err)
}

type turnExposure struct {
	bot     *Bot
	owner   string
	id      int64
	capture *agenthost.PlanCapture
}

func (s turnExposure) Reset() { s.capture.Reset() }
func (s turnExposure) Expose(ctx context.Context, input *agent.Input) error {
	return s.capture.Expose(ctx, s.owner, input)
}
func (s turnExposure) Snapshot() *interaction.PlanAuthority { return s.capture.Snapshot() }
func (s turnExposure) Validate(ctx context.Context, plan interaction.SavedPlan) error {
	return s.bot.planAuthorization().ValidatePlan(ctx, s.owner, s.id, plan)
}
