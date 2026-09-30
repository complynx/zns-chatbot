package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Recovery resumes an exact authorized receipt without executing its command.
// A missing receipt leaves normal source validation and archival unchanged.
func (b *Bot) recoverSavedPlan(ctx context.Context, in incoming, id int64) (bool, error) {
	plan, err := (interaction.Store{DB: b.DB}).Load(ctx, in.owner, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if plan.Kind != interaction.CommandPlan || plan.State != interaction.Ready {
		return false, nil
	}
	source, err := savedPlanSource(plan)
	if err != nil {
		return false, err
	}
	text, found, err := b.savedCommandReceipt(ctx, in, id, plan, source)
	if err != nil || !found {
		return found, err
	}
	// Only command coordinates and consumed-voice cleanup identifiers survive.
	// Cached prose, source-dependent menus and media interpretations never render.
	safe := interaction.SavedPlan{MediaID: plan.MediaID, AVIDs: plan.AVIDs}
	switch {
	case plan.RegistrationCommand != nil || plan.RegistrationAssignment != nil:
		safe.Plan.View = agent.RegistrationView
		safe.RegistrationCommand, safe.RegistrationAssignment = plan.RegistrationCommand, plan.RegistrationAssignment
	case plan.ProfileCommand != nil:
		safe.Plan.View = agent.ProfilesView
		safe.ProfileCommand = plan.ProfileCommand
	case plan.OrderCommand != nil:
		safe.Plan.View = agent.OrdersView
	case plan.KnowledgeCommand != nil:
		safe.Plan.View = agent.KnowledgeView
	default:
		safe.Plan.View = scriptWorkflowView
	}
	return true, b.finishAgentPlan(ctx, in, id, safe, interaction.Reply{Text: text, Origin: interaction.TrustedReply})
}

func (b *Bot) savedCommandReceipt(ctx context.Context, in incoming, id int64, plan interaction.SavedPlan,
	source readsource.Derivation) (string, bool, error) {
	switch {
	case plan.OrderCommand != nil:
		return b.savedOrderReceipt(ctx, in, id, plan, source)
	case plan.ProfileCommand != nil:
		return b.savedProfileReceipt(ctx, in, id, plan, source)
	case plan.RegistrationCommand != nil:
		command := *plan.RegistrationCommand
		if command.Key == "" {
			command.Key = "tg-registration-" + strconv.FormatInt(id, 10)
		}
		receipt, err := (interaction.RegistrationExecutor{Receipts: b.Host, Writer: b.registrationExecutionWriter(id)}).
			RecoverCommand(ctx, in.owner, command, source)
		if err != nil || !receipt.Found {
			return "", receipt.Found, err
		}
		text, err := b.registrationExecutionNotice(ctx, in, nil)
		return text, true, err
	case plan.RegistrationAssignment != nil:
		command := *plan.RegistrationAssignment
		if command.Key == "" {
			command.Key = "tg-admin-assignment-" + strconv.FormatInt(id, 10)
		}
		receipt, err := (interaction.RegistrationExecutor{Receipts: b.Host, Writer: b.registrationExecutionWriter(id)}).
			RecoverAssignment(ctx, in.owner, command, source)
		if err != nil || !receipt.Found {
			return "", receipt.Found, err
		}
		text, err := b.registrationExecutionNotice(ctx, in, nil)
		return text, true, err
	case plan.KnowledgeCommand != nil:
		return b.savedKnowledgeReceipt(ctx, in, id, plan, source)
	case plan.Plan.Action != nil:
		action := plan.Plan.Action
		command := workflow.Action{
			Name:    action.Name,
			SlotID:  action.SlotID,
			Version: plan.Version,
			Origin:  originAgent,
			Key:     "tg-" + strconv.FormatInt(id, 10),
		}
		receipt, err := b.Host.WorkflowReceipt(ctx, in.owner, command, source)
		if err != nil || !receipt.Found {
			return "", receipt.Found, err
		}
		text, err := b.workflowOutcome(ctx, in.owner, id, command, receipt.Result, nil)
		return text, true, err
	default:
		return "", false, nil
	}
}

func (b *Bot) savedOrderReceipt(ctx context.Context, in incoming, id int64, plan interaction.SavedPlan,
	source readsource.Derivation) (string, bool, error) {
	command := *plan.OrderCommand
	if command.Name == actionExport || command.Name == actionInstructions {
		return "", false, nil
	}
	command.Key = "tg-order-" + strconv.FormatInt(id, 10)
	receipt, err := b.Host.OrderReceipt(ctx, in.owner, command, source)
	if core.IsDatabaseFailure(err) {
		return "", receipt.Found, err
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Code == mediaForbidden {
		coordinator := interaction.OrderCoordinator{Store: interaction.Store{DB: b.DB}}
		outcome, recordErr := coordinator.RecordRefusal(ctx, in.owner, id, problem)
		if recordErr != nil {
			return "", true, recordErr
		}
		text, noticeErr := b.orderOutcome(ctx, in.owner, id, outcome)
		return text, true, noticeErr
	}
	if err != nil || !receipt.Found {
		return "", receipt.Found, err
	}
	if err = b.rememberOrderLocale(ctx, in.owner, id); err != nil {
		return "", true, err
	}
	text, err := b.orderOutcome(ctx, in.owner, id, interaction.OrderOutcome{Order: receipt.Result})
	return text, true, err
}

func (b *Bot) savedProfileReceipt(ctx context.Context, in incoming, id int64, plan interaction.SavedPlan,
	source readsource.Derivation) (string, bool, error) {
	command := *plan.ProfileCommand
	command.Key = "tg-profile-" + strconv.FormatInt(id, 10)
	receipt, err := b.Host.PassProfileReceipt(ctx, in.owner, command, source)
	if err != nil || !receipt.Found {
		return "", receipt.Found, err
	}
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", true, err
	}
	text, err := b.profileOutcome(ctx, in, id, command, receipt.Result, nil, preference.Language)
	return text, true, err
}

func (b *Bot) savedKnowledgeReceipt(ctx context.Context, in incoming, id int64, plan interaction.SavedPlan,
	source readsource.Derivation) (string, bool, error) {
	command := *plan.KnowledgeCommand
	if command.Name == knowledgeReviewCard {
		return "", false, nil
	}
	if command.Key == "" {
		command.Key = "tg-knowledge-" + strconv.FormatInt(id, 10)
	}
	receipt, err := b.Host.KnowledgeReceipt(ctx, in.owner, command, source)
	if err != nil || !receipt.Found {
		return "", receipt.Found, err
	}
	// The saved turn stays Ready after completion, so a lost-reply retry and a
	// completed replay both arrive here after the receipt's current source and
	// permission checks. The knowledge reply is recorded only after original-source
	// linkage succeeds; once it exists, replay is handled here and never enters
	// attachment, the command endpoint, assessment or the pending continuation.
	reply, completed, err := b.recordedKnowledgeReply(ctx, in.owner, id)
	if err != nil {
		return "", true, err
	}
	if completed {
		return b.completedKnowledgeReceiptNotice(ctx, in, command, receipt.Result, reply)
	}
	if err = b.knowledgeCoordinator().FinalizeCommand(ctx, in.owner, id, command, receipt.Result); err != nil {
		return "", true, err
	}
	return b.knowledgeReceiptNotice(ctx, in, command, receipt.Result)
}

// recordedKnowledgeReply reads the one durable reply of this owner and update
// (unique by owner, update and kind). It is catalog text written by the bot.
func (b *Bot) recordedKnowledgeReply(ctx context.Context, owner string, id int64) (string, bool, error) {
	var raw json.RawMessage
	err := b.DB.QueryRow(
		ctx,
		`SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		id,
		knowledgeReply,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, core.DatabaseOperationContextError(ctx, err)
	}
	var text string
	if err = json.Unmarshal(raw, &text); err != nil {
		return "", true, err
	}
	return text, true, nil
}
