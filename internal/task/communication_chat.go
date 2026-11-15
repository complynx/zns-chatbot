package task

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func (s *Service) communicationPersonalizationChat(ctx context.Context, auth loom.Auth, id uuid.UUID) (string, error) {
	chat, create, err := s.store.ClaimCommunicationChat(ctx, id)
	if err != nil {
		return "", err
	}
	if !create {
		if chat == "" {
			return "", errCommunicationPersonalizationFailed
		}
		return chat, nil
	}
	chat, err = s.loom.CreateChat(ctx, auth)
	if err != nil {
		var upstream *loom.Error
		if errors.As(err, &upstream) && (upstream.Outcome == loom.Rejected || upstream.Outcome == loom.NotStarted) {
			if resetErr := s.store.SaveCommunicationChat(ctx, id, ""); resetErr != nil {
				return "", resetErr
			}
			return "", err
		}
		// Chat creation has no replay contract. No recipient memory was read yet.
		return "", errCommunicationPersonalizationFailed
	}
	return chat, s.store.SaveCommunicationChat(ctx, id, chat)
}
