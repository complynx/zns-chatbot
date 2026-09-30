package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const knowledgePrefix = "knowledge:"
const knowledgeStateKind = "knowledge_view"
const knowledgeReply = "knowledge_reply"
const knowledgeMemoMode = "memos"
const knowledgeOwnMode = "own"
const knowledgeReviewMode = "review"
const knowledgeFactsMode = "facts"
const knowledgeRetryAssessment = "retry_assessment"

type knowledgeView struct {
	Cursor string `json:"cursor,omitempty"`
	Event  string `json:"event"`
	Mode   string `json:"mode"`
	After  int64  `json:"after"`
}
type knowledgeButton struct {
	View       knowledgeView         `json:"view"`
	Command    *knowledge.Command    `json:"command,omitempty"`
	Submission *knowledge.Submission `json:"submission,omitempty"`
}

// A callback is an opaque reference to a command the host showed this owner.
// It cannot carry a new actor, version, permission or arbitrary business payload.
func (b *Bot) knowledgeButton(
	ctx context.Context,
	owner, label string,
	value knowledgeButton,
) (telegram.Button, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return telegram.Button{}, err
	}
	digest := sha256.Sum256(append([]byte(owner+":"), data...))
	const tokenBytes = 16
	token := hex.EncodeToString(digest[:tokenBytes])
	if err = b.record(ctx, owner, 0, knowledgePrefix+token, value); err != nil {
		return telegram.Button{}, err
	}
	return telegram.Button{Text: label, Data: knowledgePrefix + token}, nil
}

func (b *Bot) saveKnowledgeView(ctx context.Context, owner string, view knowledgeView) error {
	_, err := b.DB.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,0,$2,$3)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, knowledgeStateKind, view)
	return err
}

func (b *Bot) currentKnowledgeView(ctx context.Context, owner string) (knowledgeView, error) {
	view := knowledgeView{Mode: knowledgeFactsMode}
	var content []byte
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=0 AND kind=$2`, owner, knowledgeStateKind).
		Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return view, core.DatabaseOperationError(err)
	}
	view = knowledgeView{}
	err = json.Unmarshal(content, &view)
	return view, err
}

func (b *Bot) handleKnowledge(ctx context.Context, in incoming, update telegram.Update) error {
	notice := ""
	if update.Callback == nil {
		view := knowledgeView{Mode: knowledgeFactsMode}
		if in.text == "/memo" {
			view.Mode = knowledgeMemoMode
		}
		if err := b.saveKnowledgeView(ctx, in.owner, view); err != nil {
			return err
		}
	} else {
		var err error
		notice, err = b.executeKnowledgeButton(ctx, in, update.ID)
		if err != nil {
			return err
		}
	}
	if err := b.record(ctx, in.owner, update.ID, knowledgeReply, notice); err != nil {
		return err
	}
	if err := b.RenderKnowledge(ctx, in.owner, in.chat); err != nil {
		return err
	}
	if update.Callback != nil {
		return b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

func (b *Bot) executeKnowledgeButton(ctx context.Context, in incoming, id int64) (string, error) {
	var value knowledgeButton
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=0 AND kind=$2`, in.owner, in.text).
		Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) || !strings.HasPrefix(in.text, knowledgePrefix) {
		preference, prefErr := b.API.Preferences(ctx, in.owner)
		if prefErr != nil {
			return "", prefErr
		}
		return i18n.Translate(preference.Language, i18n.KnowledgeUnavailable, nil)
	}
	if err != nil {
		return "", err
	}
	if value.Submission != nil {
		return b.submitKnowledgeProposal(ctx, in, *value.Submission)
	}
	if value.Command != nil {
		command := *value.Command
		if command.Name == knowledgeRetryAssessment {
			return b.retryKnowledgeAssessmentNotice(ctx, in, id, command)
		}
		command.Key = "tg-knowledge-button-" + strings.TrimPrefix(in.text, knowledgePrefix)
		return b.executeKnowledgeCommand(ctx, in, id, command)
	}
	return "", b.saveKnowledgeView(ctx, in.owner, value.View)
}

func (b *Bot) retryKnowledgeAssessmentNotice(
	ctx context.Context,
	in incoming,
	id int64,
	command knowledge.Command,
) (string, error) {
	assessment, assessmentErr := b.knowledgeCoordinator().RetryAssessment(ctx, in.owner, id, command)
	if assessmentErr != nil {
		return "", assessmentErr
	}
	if assessment.Status == interaction.KnowledgeAssessmentDeferred {
		preference, preferenceErr := b.API.Preferences(ctx, in.owner)
		if preferenceErr != nil {
			return "", preferenceErr
		}
		if saveErr := b.saveKnowledgeView(
			ctx,
			in.owner,
			knowledgeView{Event: command.Event, Mode: knowledgeOwnMode},
		); saveErr != nil {
			return "", saveErr
		}
		return i18n.Translate(preference.Language, i18n.KnowledgePendingFilter, nil)
	}
	return "", b.saveKnowledgeView(ctx, in.owner, knowledgeView{Event: command.Event, Mode: knowledgeOwnMode})
}
