package task

import (
	"context"
	"fmt"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

// Call after an import releases its connection, or before a dispatch locks the owner.
// The IAM adapter receives explicit evidence; it never borrows a DB connection.
func (s *Service) profileIAMAccount(
	ctx context.Context, scope db.ProfileScope, user db.User,
) (identity.Account, error) {
	account := identity.Account{
		Subject: user.Subject, OrganizationID: scope.Organization,
		SyntheticEmail: fmt.Sprintf("tg+%d+%d@telegram.invalid", scope.BotID, user.TelegramUserID),
	}
	providerIssuer, providerID := s.iam.TelegramProfileLinkPins()
	if providerIssuer == "" && providerID == "" {
		return account, nil
	}
	link, err := s.store.CompletedTelegramProfileLink(ctx, scope, providerIssuer, providerID)
	if err != nil {
		return identity.Account{}, err
	}
	if link != nil {
		account.TelegramLink = &identity.TelegramProfileLink{
			ProviderIssuer: link.ProviderIssuer, ProviderID: link.ProviderID,
			ExternalSubject: link.ExternalSubject, Subject: link.Subject, Organization: link.Organization,
			BotID: link.BotID, TelegramID: link.TGID, CompletedAt: link.CompletedAt, ExpiresAt: link.ExpiresAt,
		}
	}
	return account, nil
}
