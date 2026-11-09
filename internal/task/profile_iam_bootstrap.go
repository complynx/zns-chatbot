package task

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

// An unavailable optional IAM name read must not block an already authorized
// conversation. The missing local name can still be requested explicitly.
func (s *Service) importIAMName(ctx context.Context, scope db.ProfileScope, user db.User) (db.UserProfile, error) {
	profile, err := s.store.ImportIAMProfileName(ctx, scope, func(ctx context.Context) (string, string, error) {
		account, accountErr := s.profileIAMAccount(ctx, scope, user)
		if accountErr != nil {
			return "", "", accountErr
		}
		name, readErr := s.iam.ReadProfileName(ctx, account, user.TelegramUserID)
		return name.FirstName, name.LastName, readErr
	})
	if errors.Is(err, identity.ErrUnavailable) {
		return s.store.GetProfile(ctx, scope)
	}
	return profile, err
}
