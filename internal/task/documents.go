package task

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func (s *Service) sendOutbound(ctx context.Context, row db.Delivery) (telegram.Receipt, string, error) {
	var header struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(row.Payload, &header) != nil {
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.Rejected}
	}
	switch header.Kind {
	case "avatar_document":
		receipt, err := s.sendAvatarDocument(ctx, row)
		return receipt, "", err
	case "":
		return s.sendKeyboardText(ctx, row)
	default:
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.Rejected}
	}
}

func (s *Service) sendText(ctx context.Context, raw json.RawMessage) (telegram.Receipt, error) {
	var payload telegram.SendMessageRequest
	if json.Unmarshal(raw, &payload) != nil {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.Rejected}
	}
	receipt, err := s.tg.SendMessage(ctx, payload)
	var rejected *telegram.Error
	if errors.As(err, &rejected) && rejected.InvalidEntities && rejected.Outcome == telegram.Rejected &&
		len(payload.Entities) > 0 {
		payload.Entities = nil
		return s.tg.SendMessage(ctx, payload)
	}
	return receipt, err
}

func (s *Service) sendAvatarDocument(ctx context.Context, row db.Delivery) (telegram.Receipt, error) {
	var input struct {
		ArtifactID uuid.UUID `json:"artifact_id"`
		ChatID     int64     `json:"chat_id"`
	}
	if json.Unmarshal(row.Payload, &input) != nil || input.ArtifactID == uuid.Nil {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.Rejected}
	}
	t, err := s.store.GetTask(ctx, row.TaskID)
	if err != nil {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.NotStarted, Retryable: true}
	}
	u, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.NotStarted, Retryable: true}
	}
	bot, organization, _ := s.iam.DeploymentScope()
	if u.Revoked || u.BotID != bot || u.Organization != organization || u.TelegramUserID != input.ChatID {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.Rejected}
	}
	_, content, err := s.store.GetAvatarForDelivery(ctx, t.ID, input.ArtifactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.Rejected}
	}
	if err != nil {
		return telegram.Receipt{}, &telegram.Error{Outcome: telegram.NotStarted, Retryable: true}
	}
	return s.tg.SendDocument(ctx, telegram.SendDocumentRequest{
		ChatID: input.ChatID, Filename: "zinusya-avatar.jpg", Content: content,
	})
}
