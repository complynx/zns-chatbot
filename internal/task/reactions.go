package task

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const reactionInstructions = "\nYou may use message_react when a reaction would feel natural in a messenger, " +
	"for example a bashful response to being corrected, encouragement, empathy or shared excitement. " +
	"Reactions are optional; do not react to every message or substitute one for a necessary answer or action. " +
	"Target the exact history message_seq. Keep at most one active reaction per message; remove an earlier " +
	"one before replacing it. Choose only from the advertised ordinary emoji. A reaction never means consent. " +
	"This private-chat transport does not supply incoming user reactions. Your own message_react receipts " +
	"do not reveal user reactions. Do not claim a message has no user reaction; explain that you cannot see them."

const reactionIOTimeout = 3 * time.Second
const reactionHistoryLimit = 50
const reactionAuthRetry = 5 * time.Second

func (s *Service) registerRunReactions(ctx context.Context, t *db.Task, c *checkpoint, run loom.Run) error {
	if run.ChatID == "" || c.ReactionChatID == run.ChatID || c.Create == nil || c.Create.Interaction.Reactions == nil {
		return nil
	}
	// Mapping initial user messages must see a durable run ID before the consumer starts.
	t.RunID, c.Run = run.ID, &run
	if err := s.save(ctx, t, *c, nil); err != nil {
		return err
	}
	if err := s.store.RegisterReactionChat(ctx, t.UserID, t.ID, run.ChatID); err != nil {
		return err
	}
	c.ReactionChatID = run.ChatID
	return nil
}

func (s *Service) reactionAuth(ctx context.Context, id uuid.UUID) (loom.Auth, error) {
	u, err := s.store.GetUser(ctx, id)
	if err != nil {
		return loom.Auth{}, err
	}
	_, organization, _ := s.iam.DeploymentScope()
	if u.Revoked || u.BotID != s.options.BotID || u.Organization != organization || u.Subject == "" {
		return loom.Auth{}, identity.ErrDenied
	}
	service, err := s.iam.ServiceToken(ctx)
	if err != nil {
		return loom.Auth{}, err
	}
	subject, err := s.iam.UserToken(ctx, u.Subject)
	if err != nil {
		return loom.Auth{}, err
	}
	if _, err = s.iam.Verify(ctx, subject.Token); err != nil {
		return loom.Auth{}, err
	}
	current, err := s.store.GetUser(ctx, id)
	if err != nil {
		return loom.Auth{}, err
	}
	if current.Revoked || current.Subject != u.Subject || current.BotID != s.options.BotID ||
		current.Organization != organization {
		return loom.Auth{}, identity.ErrDenied
	}
	return loom.Auth{ServiceToken: service.Token, SubjectToken: subject.Token}, nil
}

func (s *Service) watchReactions(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshReactions(ctx)
			if err := s.store.ResolveReactionTargets(ctx, s.options.BotID); err != nil {
				s.record(ctx, "reactions", metricError)
			}
			s.dispatchReaction(ctx)
		}
	}
}

func (s *Service) refreshReactions(ctx context.Context) {
	cursors, err := s.store.DueReactionChats(ctx, s.options.BotID)
	if err != nil {
		s.record(ctx, "reactions", metricError)
		return
	}
	for _, cursor := range cursors {
		pageCtx, cancel := context.WithTimeout(ctx, reactionIOTimeout)
		err = s.refreshReactionPage(pageCtx, cursor)
		cancel()
		if err != nil && ctx.Err() == nil {
			s.record(ctx, "reactions", metricError)
			_ = s.store.DelayReactionChat(ctx, cursor)
		}
	}
}

func (s *Service) refreshReactionPage(ctx context.Context, c db.ReactionCursor) error {
	auth, err := s.reactionAuth(ctx, c.UserID)
	if err != nil {
		return err
	}
	events, err := s.loom.ChatHistory(ctx, auth, c.LoomChatID, c.After, reactionHistoryLimit)
	if err != nil {
		return err
	}
	var projections []db.ReactionProjection
	after := c.After
	for _, event := range events {
		after = event.Seq
		target := event
		if event.Type == "message.reaction" {
			var change struct {
				Target int64  `json:"message_seq"`
				Actor  string `json:"actor"`
			}
			if json.Unmarshal(event.Data, &change) != nil || change.Actor != "assistant" || change.Target <= 0 {
				continue
			}
			page, readErr := s.loom.ChatHistory(ctx, auth, c.LoomChatID, change.Target-1, 1)
			if readErr != nil {
				return readErr
			}
			if len(page) != 1 || page[0].Seq != change.Target {
				continue
			}
			target = page[0]
		}
		projection, ok, mapErr := s.projectReaction(ctx, c.UserID, target)
		if mapErr != nil {
			return mapErr
		}
		if ok {
			projections = append(projections, projection)
		}
	}
	return s.store.ReactionPage(ctx, c, after, len(events) == 0, projections)
}

func (s *Service) projectReaction(ctx context.Context, user uuid.UUID, event loom.ChatEvent) (
	db.ReactionProjection, bool, error,
) {
	if event.ReactionsSeq == 0 {
		return db.ReactionProjection{}, false, nil
	}
	var ids struct {
		ID         string `json:"id"`
		QuestionID string `json:"question_id"`
		ApprovalID string `json:"approval_id"`
	}
	_ = json.Unmarshal(event.Data, &ids)
	id := ids.ID
	switch event.Type {
	case "question.created":
		id = ids.QuestionID
	case "approval.created":
		id = ids.ApprovalID
	}
	chat, message, err := s.store.ReactionTarget(ctx, user, event.Type, event.RunID, id)
	if errors.Is(err, pgx.ErrNoRows) || event.RunID == "" {
		return db.ReactionProjection{}, false, nil
	}
	if err != nil && !errors.Is(err, db.ErrReactionTargetPending) {
		return db.ReactionProjection{}, false, err
	}
	if err == nil && (chat == 0 || message <= 0) {
		return db.ReactionProjection{}, false, nil
	}
	values := []string{}
	for _, emoji := range event.Reactions["assistant"] {
		if emoji != "" && telegram.SupportedReaction(emoji) {
			values = append(values, emoji)
		}
	}
	slices.Sort(values)
	emoji := ""
	if len(values) > 0 {
		emoji = values[len(values)-1]
	}
	return db.ReactionProjection{MessageSeq: event.Seq, Watermark: event.ReactionsSeq,
		ChatID: chat, MessageID: message, Emoji: emoji, Kind: event.Type, RunID: event.RunID, EventID: id}, true, nil
}

func (s *Service) dispatchReaction(ctx context.Context) {
	delivery, err := s.store.BeginReaction(ctx, s.options.BotID)
	if err != nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, reactionIOTimeout)
	_, err = s.reactionAuth(sendCtx, delivery.UserID)
	state, delay := "failed", time.Duration(0)
	if err == nil {
		err = s.tg.SetMessageReaction(sendCtx, delivery.ChatID, delivery.MessageID, delivery.Emoji)
		state, delay = reactionOutcome(err)
	} else if !errors.Is(err, identity.ErrDenied) {
		state, delay = deliveryPending, reactionAuthRetry
	}
	cancel()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer finishCancel()
	if err = s.store.FinishReaction(finishCtx, delivery, state, delay); err != nil {
		s.record(finishCtx, "reactions", metricError)
	}
}

func reactionOutcome(err error) (string, time.Duration) {
	if err == nil {
		return "delivered", 0
	}
	if upstream, ok := errors.AsType[*telegram.Error](err); ok {
		if upstream.Outcome == telegram.Unknown {
			return "unknown", 0
		}
		if upstream.Retryable {
			return deliveryPending, max(time.Second, upstream.RetryAfter)
		}
	}
	return "failed", 0
}
