package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

const workflowStateParameter = "workflowstate"

func workflowState(m *orderMessages, state string) string {
	id := i18n.WorkflowStateIdle
	switch state {
	case stateDraft:
		id = i18n.WorkflowStateDraft
	case stateBooked:
		id = i18n.WorkflowStateBooked
	case "cancelled":
		id = i18n.RegistrationCancelled
	}
	return m.text(id, nil)
}

func (b *Bot) workflowUpdated(ctx context.Context, owner, state string) (string, error) {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return "", err
	}
	m := &orderMessages{language: preference.Language}
	text := m.text(i18n.WorkflowUpdated, map[string]string{workflowStateParameter: workflowState(m, state)})
	return text, m.err
}

func (b *Bot) invalidWorkflowCallback(ctx context.Context, owner string, update int64) (string, error) {
	if err := b.record(ctx, owner, update, "error", "invalid_callback"); err != nil {
		return "", err
	}
	return b.orderMessage(ctx, owner, i18n.WorkflowInvalid, nil)
}

// Re-render only structured workflow outcomes. Free-form agent replies keep their text.
func (b *Bot) localizeWorkflowNotice(
	ctx context.Context,
	owner string,
	update int64,
	language, fallback string,
) (string, error) {
	var raw []byte
	var kind string
	err := b.DB.QueryRow(ctx, `SELECT kind,content FROM bot.interactions
 WHERE owner=$1 AND update_id=$2 AND kind IN ('result','error') ORDER BY id DESC LIMIT 1`, owner, update).
		Scan(&kind, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	if kind == "error" {
		var code string
		if err = json.Unmarshal(raw, &code); err != nil {
			return "", err
		}
		if code == "invalid_callback" {
			return i18n.Translate(language, i18n.WorkflowInvalid, nil)
		}
		return fallback, nil
	}
	var result struct {
		Workflow *workflow.Workflow `json:"workflow"`
		Error    string             `json:"error"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return i18n.Translate(language, i18n.WorkflowFailed, map[string]string{"code": result.Error})
	}
	if result.Workflow == nil {
		return fallback, nil
	}
	m := &orderMessages{language: language}
	text := m.text(
		i18n.WorkflowUpdated,
		map[string]string{workflowStateParameter: workflowState(m, result.Workflow.State)},
	)
	return text, m.err
}

func (b *Bot) refreshOpenWorkflow(ctx context.Context, owner string, chat int64) error {
	var exists bool
	if err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.messages WHERE owner=$1)`, owner).
		Scan(&exists); err != nil {
		return err
	}
	if exists {
		return b.Render(ctx, owner, chat)
	}
	return nil
}
