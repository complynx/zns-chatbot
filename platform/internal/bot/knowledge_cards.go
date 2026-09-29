package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type knowledgeRenderer struct {
	bot             *Bot
	owner, language string
	chat            int64
	view            knowledgeView
	keys            map[string]bool
}

const knowledgeRejectDecision = "reject"

func (r *knowledgeRenderer) text(id i18n.ID) string {
	value, _ := i18n.Translate(r.language, id, nil)
	return value
}
func (r *knowledgeRenderer) card(ctx context.Context, key, text string, rows [][]telegram.Button) error {
	key = knowledgePrefix + key
	r.keys[key] = true
	return r.bot.deliverOrderCard(
		ctx,
		r.owner,
		key,
		telegram.Send{ChatID: r.chat, Text: text, Markup: telegram.Markup{Rows: rows}},
	)
}
func (r *knowledgeRenderer) button(ctx context.Context, id i18n.ID, value knowledgeButton) (telegram.Button, error) {
	return r.bot.knowledgeButton(ctx, r.owner, r.text(id), value)
}

func (b *Bot) RenderKnowledge(ctx context.Context, owner string, chat int64) error {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	view, err := b.currentKnowledgeView(ctx, owner)
	if err != nil {
		return err
	}
	scopes, err := b.API.KnowledgeScopes(ctx, owner)
	if err != nil {
		return err
	}
	r := knowledgeRenderer{
		bot:      b,
		owner:    owner,
		language: preference.Language,
		chat:     chat,
		view:     view,
		keys:     map[string]bool{},
	}
	if err = r.navigation(ctx, scopes); err != nil {
		return err
	}
	switch view.Mode {
	case knowledgeMemoMode:
		err = r.memos(ctx)
	case knowledgeOwnMode:
		err = r.proposals(ctx, false)
	case knowledgeReviewMode:
		if knowledgeCapability(scopes, view.Event, false) {
			err = r.proposals(ctx, true)
		}
	default:
		err = r.facts(ctx)
	}
	if err != nil {
		return err
	}
	return r.retire(ctx)
}

func (r *knowledgeRenderer) navigation(ctx context.Context, scopes []knowledge.Scope) error {
	rows := [][]telegram.Button{}
	for _, scope := range scopes {
		label := scope.Event
		if label == "" {
			label = r.text(i18n.KnowledgeGeneral)
		} else if scope.Phase == "past" {
			label += " · " + r.text(i18n.KnowledgePast)
		}
		button, err := r.bot.knowledgeButton(
			ctx,
			r.owner,
			label,
			knowledgeButton{View: knowledgeView{Event: scope.Event, Mode: knowledgeFactsMode}},
		)
		if err != nil {
			return err
		}
		rows = append(rows, []telegram.Button{button})
	}
	modes := []struct {
		id   i18n.ID
		mode string
	}{{i18n.KnowledgeMemos, knowledgeMemoMode}, {i18n.KnowledgeOwn, knowledgeOwnMode}}
	if knowledgeCapability(scopes, r.view.Event, false) {
		modes = append(modes, struct {
			id   i18n.ID
			mode string
		}{i18n.KnowledgeReview, knowledgeReviewMode})
	}
	for _, mode := range modes {
		button, err := r.button(
			ctx,
			mode.id,
			knowledgeButton{View: knowledgeView{Event: r.view.Event, Mode: mode.mode}},
		)
		if err != nil {
			return err
		}
		rows = append(rows, []telegram.Button{button})
	}
	var notice string
	var raw json.RawMessage
	var updateID int64
	err := r.bot.DB.QueryRow(ctx, `SELECT content,update_id FROM bot.interactions WHERE owner=$1 AND kind=$2 ORDER BY id DESC LIMIT 1`, r.owner, knowledgeReply).
		Scan(&raw, &updateID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		visible, visibleErr := r.bot.historyReplyVisible(ctx, r.owner, updateID)
		if visibleErr != nil {
			return visibleErr
		}
		if !visible {
			raw = nil
		}
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &notice); err != nil {
			return err
		}
	}
	return r.card(ctx, "main", r.text(i18n.KnowledgeTitle)+"\n"+r.text(i18n.KnowledgeHint)+"\n\n"+notice, rows)
}

func (r *knowledgeRenderer) facts(ctx context.Context) error {
	page, err := r.bot.API.KnowledgePage(ctx, r.owner, knowledge.Query{Event: r.view.Event, Cursor: r.view.Cursor})
	if err != nil {
		return err
	}
	for _, fact := range page.Facts {
		source := r.text(i18n.KnowledgeGeneral)
		if fact.Event != "" {
			source = fact.Event + " · " + r.text(i18n.KnowledgeUpcoming)
			if fact.Phase == "past" {
				source = fact.Event + " · " + r.text(i18n.KnowledgePast)
			}
		}
		key := "fact:" + fact.Event + ":" + fact.Topic + ":" + fact.Key
		if err = r.card(ctx, key, source+"\n"+fact.Text, nil); err != nil {
			return err
		}
	}
	if page.More {
		view := r.view
		view.Cursor = page.NextCursor
		button, buttonErr := r.button(ctx, i18n.PageNext, knowledgeButton{View: view})
		if buttonErr != nil {
			return buttonErr
		}
		return r.card(ctx, "next", r.text(i18n.PageNext), [][]telegram.Button{{button}})
	}
	return nil
}

func (r *knowledgeRenderer) memos(ctx context.Context) error {
	memos, err := r.bot.API.Memos(ctx, r.owner)
	if err != nil {
		return err
	}
	if len(memos) == 0 {
		return r.card(ctx, "memos-empty", r.text(i18n.KnowledgeMemos)+"\n"+r.text(i18n.KnowledgeEmpty), nil)
	}
	for _, memo := range memos {
		button, buttonErr := r.button(
			ctx,
			i18n.KnowledgeDelete,
			knowledgeButton{
				Command: &knowledge.Command{Name: knowledge.MemoDelete, FactKey: memo.Key, Version: memo.Version},
			},
		)
		if buttonErr != nil {
			return buttonErr
		}
		if err = r.card(ctx, "memo:"+memo.Key, memo.Text, [][]telegram.Button{{button}}); err != nil {
			return err
		}
	}
	return nil
}

func (r *knowledgeRenderer) proposals(ctx context.Context, review bool) error {
	proposals, err := r.bot.API.KnowledgeProposals(
		ctx,
		r.owner,
		knowledge.ProposalQuery{Event: r.view.Event, ReviewQueue: review, After: r.view.After},
	)
	if err != nil {
		return err
	}
	for _, proposal := range proposals {
		if err = r.proposalCard(ctx, proposal, review); err != nil {
			return err
		}
	}
	if len(proposals) == knowledge.MaxResults {
		view := r.view
		view.After = proposals[len(proposals)-1].ID
		button, buttonErr := r.button(ctx, i18n.PageNext, knowledgeButton{View: view})
		if buttonErr != nil {
			return buttonErr
		}
		return r.card(ctx, "next", r.text(i18n.PageNext), [][]telegram.Button{{button}})
	}
	return nil
}

func (r *knowledgeRenderer) proposalCard(ctx context.Context, p knowledge.Proposal, review bool) error {
	rows := [][]telegram.Button{}
	if !review && p.State == knowledgePendingFilter {
		button, err := r.button(
			ctx,
			i18n.KnowledgeRetry,
			knowledgeButton{
				Command: &knowledge.Command{
					Name:       knowledgeRetryAssessment,
					Event:      p.Event,
					ProposalID: p.ID,
					Version:    p.Version,
				},
			},
		)
		if err != nil {
			return err
		}
		rows = append(rows, []telegram.Button{button})
	}
	if review {
		for _, choice := range []struct {
			id       i18n.ID
			decision string
		}{{i18n.KnowledgeApprove, "approve"}, {i18n.KnowledgeReject, knowledgeRejectDecision}} {
			button, err := r.button(
				ctx,
				choice.id,
				knowledgeButton{
					Command: &knowledge.Command{
						Name:       knowledge.Review,
						Event:      p.Event,
						ProposalID: p.ID,
						Version:    p.Version,
						Decision:   choice.decision,
					},
				},
			)
			if err != nil {
				return err
			}
			rows = append(rows, []telegram.Button{button})
		}
	}
	status := map[string]i18n.ID{knowledgePendingFilter: i18n.KnowledgePendingFilter, "pending_review": i18n.KnowledgePendingReview, "filtered": i18n.KnowledgeFiltered, "approved": i18n.KnowledgeApproved, "rejected": i18n.KnowledgeRejected}[p.State]
	scope := p.Event
	if scope == "" {
		scope = r.text(i18n.KnowledgeGeneral)
	}
	text := fmt.Sprintf("%s\n%s\n\n%s", scope, p.Text, r.text(status))
	if p.Reason != "" {
		text += "\n" + p.Reason
	}
	return r.card(ctx, "proposal:"+strconv.FormatInt(p.ID, 10), text, rows)
}

func (r *knowledgeRenderer) retire(ctx context.Context) error {
	rows, err := r.bot.DB.Query(
		ctx,
		`SELECT card_key FROM bot.order_cards WHERE owner=$1 AND card_key LIKE 'knowledge:%'`,
		r.owner,
	)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, key := range keys {
		if !r.keys[key] {
			if err = r.bot.deliverOrderCard(
				ctx,
				r.owner,
				key,
				telegram.Send{ChatID: r.chat, Text: r.text(i18n.KnowledgeClosed)},
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Bot) reconcileKnowledgeViews(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.order_cards WHERE card_key='knowledge:main'`)
	if err != nil {
		return err
	}
	type view struct {
		Owner string
		Chat  int64
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[view])
	if err != nil {
		return err
	}
	for _, item := range views {
		viewContext, authErr := b.API.notificationContext(ctx, item.Owner, item.Chat)
		if authErr != nil {
			b.logger().WarnContext(ctx, "knowledge view identity pending")
			continue
		}
		if err = b.RenderKnowledge(viewContext, item.Owner, item.Chat); err != nil {
			b.logger().WarnContext(ctx, "knowledge view reconciliation pending")
		}
	}
	return nil
}
