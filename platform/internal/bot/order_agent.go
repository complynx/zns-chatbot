package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (b *Bot) handleAgentUpdate(ctx context.Context, in incoming, id int64) error {
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
		if err = b.executeMediaPlan(ctx, in, cached); err != nil {
			return err
		}
		return b.finishConsumedVoice(ctx, in.owner, cached)
	}
	notice, err := b.executePlan(ctx, in, id, cached)
	if err != nil {
		return err
	}
	if cached.Plan.View == agent.RegistrationView {
		return b.finishRegistrationReply(ctx, in, id, cached, notice)
	}
	kind := "reply"
	var reply any = notice
	switch cached.Plan.View {
	case agent.KnowledgeView:
		kind = knowledgeReply
	case agent.OrdersView:
		kind = "orders_reply"
	case agent.ProfilesView:
		kind = "profile_reply"
		if cached.ProfileCommand == nil {
			kind = profileAnswer
			reply = profileAnswerRecord{Version: cached.ProfileVersion, Text: notice}
		}
	}
	native := cached.OrderCommand == nil && cached.ProfileCommand == nil && cached.KnowledgeCommand == nil &&
		cached.Plan.Action == nil && cached.SystemNotice == ""
	if err = b.recordReply(ctx, in.owner, id, kind, reply, native); err != nil {
		return err
	}
	if err = b.finishConsumedVoice(ctx, in.owner, cached); err != nil {
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
	input.View = "workflow"
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
	list, err := b.API.Orders(ctx, owner, b.currentOrderEvent())
	if err != nil {
		return err
	}
	input.OrderCount = len(list)
	for _, order := range list {
		if order.State == stateUnpaid || order.State == stateCash {
			input.EditableOrderCount++
		}
	}
	input.Orders = orderSummaries(list, currentRequestEvidence(*input), input.EditableOrderCount)
	event, err := b.API.OrderEvent(ctx, owner, b.currentOrderEvent())
	if problem, ok := errors.AsType[*core.ProblemError](err); ok &&
		problem.Status == http.StatusNotFound && problem.Code == "event_not_found" {
		// Food and pass events can exist without the optional modern orders domain.
		return nil
	}
	if err != nil {
		return err
	}
	input.Extras = event.Extras
	input.OrderHistory, err = b.API.OrderHistory(ctx, owner, b.currentOrderEvent())
	return err
}

// Keep recent summaries and any explicitly named order. Full choices are loaded
// only when executing a version-bound proposal, never copied from model context.
func orderSummaries(list []orders.Order, text string, editableCount int) []agent.OrderSummary {
	const recentOrders = 5
	const maxSummaries = 10
	result := []agent.OrderSummary{}
	for index, order := range slices.Backward(list) {
		singleEditable := editableCount == 1 && (order.State == stateUnpaid || order.State == stateCash)
		if index < len(list)-recentOrders && !strings.Contains(text, order.ID) && !singleEditable {
			continue
		}
		result = append(result, agent.OrderSummary{ID: order.ID, Version: order.Version, State: order.State,
			Choice: orders.Choice{Extras: order.Choice.Extras, Total: order.Choice.Total}})
		if len(result) == maxSummaries {
			break
		}
	}
	return result
}

func (b *Bot) proposedOrderCommand(
	ctx context.Context,
	owner string,
	proposal *agent.OrderProposal,
	input agent.Input,
) (*orders.Command, error) {
	command := &orders.Command{EventID: b.currentOrderEvent(), Origin: originAgent}
	if proposal.Name == actionInstructions {
		return b.proposedInstructions(proposal, input)
	}
	if proposal.Name == actionExport {
		command.Name = actionExport
		return command, nil
	}
	if proposal.Name == actionCreateOrder {
		command.Name, command.Choice = actionCreateOrder, &orders.ChoiceInput{}
		return command, nil
	}
	extra, exists := input.Extras[proposal.Extra]
	if !exists || extra.Legacy {
		return nil, errors.New("unknown order extra")
	}
	if input.EditableOrderCount > 1 && !strings.Contains(currentRequestEvidence(input), proposal.OrderID) {
		return nil, errors.New("explicit order selection required")
	}
	for _, order := range input.Orders {
		if order.ID != proposal.OrderID {
			continue
		}
		if order.State != stateUnpaid && order.State != stateCash {
			return nil, errors.New("order is not editable")
		}
		current, err := b.API.Order(ctx, owner, b.currentOrderEvent(), order.ID)
		if err != nil {
			return nil, err
		}
		if current.Version != order.Version {
			return nil, errors.New("order changed while planning")
		}
		choice := choiceInput(current.Choice)
		if proposal.Name == "remove_extra" {
			delete(choice.Extras, proposal.Extra)
		} else {
			choice.Extras[proposal.Extra] = json.RawMessage(`0`)
		}
		command.Name, command.OrderID, command.Version, command.Choice = "edit", order.ID, order.Version, &choice
		return command, nil
	}
	return nil, errors.New("unknown order")
}

func (b *Bot) proposedInstructions(proposal *agent.OrderProposal, input agent.Input) (*orders.Command, error) {
	if input.OrderCount != 1 && !strings.Contains(currentRequestEvidence(input), proposal.OrderID) {
		return nil, errors.New("explicit order selection required")
	}
	return &orders.Command{
		EventID: b.currentOrderEvent(),
		Origin:  originAgent,
		Name:    actionInstructions,
		OrderID: proposal.OrderID,
	}, nil
}
