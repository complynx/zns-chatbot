package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func (b *Bot) handleAgentUpdate(ctx context.Context, in incoming, id int64) error {
	if handled, err := b.recoverSavedPlan(ctx, in, id); handled || err != nil {
		return err
	}
	cached, err := b.planForUpdate(ctx, in, id)
	if err != nil {
		return err
	}
	if err = b.refreshScriptProfileEffects(ctx, in, id); err != nil {
		return err
	}
	if err = b.archiveUserRequest(ctx, in, id, cached); err != nil {
		return err
	}
	if err = b.finalizeMemorySources(ctx, in.owner, id); err != nil {
		return err
	}
	if err = b.record(
		ctx,
		in.owner,
		id,
		"input",
		map[string]string{originField: in.origin},
	); err != nil {
		return err
	}
	if cached.Plan.View == agent.MediaView {
		if err = b.executeMediaPlan(ctx, in, id, cached); err != nil {
			return err
		}
		return b.finishConsumedVoice(ctx, in.owner, cached)
	}
	notice, err := b.executePlan(ctx, in, id, cached)
	if err != nil {
		return err
	}
	return b.finishAgentPlan(ctx, in, id, cached, notice)
}

func (b *Bot) finishAgentPlan(ctx context.Context, in incoming, id int64,
	cached interaction.SavedPlan, notice interaction.Reply) error {
	if cached.Plan.View == agent.RegistrationView {
		return b.finishRegistrationReply(ctx, in, id, cached, notice)
	}
	kind := historyReply
	var reply any = notice.Text
	switch cached.Plan.View {
	case agent.KnowledgeView:
		kind = knowledgeReply
	case agent.OrdersView:
		kind = "orders_reply"
	case agent.ProfilesView:
		kind = "profile_reply"
		if cached.ProfileCommand == nil {
			kind = profileAnswer
			reply = profileAnswerRecord{Version: cached.ProfileVersion, Text: notice.Text}
		}
	}
	native := notice.Origin == interaction.DerivedReply
	if err := b.recordReply(ctx, in.owner, id, kind, reply, native, notice.Origin); err != nil {
		return err
	}
	if err := b.finishConsumedVoice(ctx, in.owner, cached); err != nil {
		return err
	}
	if cached.Plan.View == agent.OrdersView {
		return b.RenderOrders(ctx, in.owner, in.chat)
	}
	if cached.Plan.View == agent.KnowledgeView {
		return b.RenderKnowledge(ctx, in.owner, in.chat)
	}
	if cached.Plan.View == agent.ProfilesView {
		return b.RenderProfile(ctx, in.owner, in.chat)
	}
	return b.Render(ctx, in.owner, in.chat)
}

func (b *Bot) addOrderContext(ctx context.Context, owner string, input *agent.Input) error {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	input.Language = string(i18n.NormalizeLocale(preference.Language))
	input.View = scriptWorkflowView
	var kind string
	err = b.DB.QueryRow(ctx, `SELECT kind FROM bot.interactions WHERE owner=$1 AND kind IN ('reply','orders_reply','profile_reply','profile_answer') ORDER BY id DESC LIMIT 1`, owner).
		Scan(&kind)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	switch kind {
	case "orders_reply":
		input.View = agent.OrdersView
	case "profile_reply", profileAnswer:
		input.View = agent.ProfilesView
	}
	if input.AV != nil {
		input.View = agent.MediaView
	}
	return (interaction.OrderCoordinator{Client: b.API, EventID: b.currentOrderEvent()}).AddContext(
		ctx,
		owner,
		input,
		agenthost.CurrentRequestEvidence(*input),
	)
}
