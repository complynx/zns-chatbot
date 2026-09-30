package account

import (
	"context"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/broadcastprofile"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// SenderMetadata is the authenticated transport sender, never model input.
type SenderMetadata struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name,omitempty"`
	Username     string `json:"username,omitempty"`
	IsBot        bool   `json:"is_bot"`
	LanguageCode string `json:"language_code,omitempty"`
}

type TelegramMetadataUpdate struct {
	Sender   SenderMetadata `json:"sender"`
	UpdateID int64          `json:"update_id"`
}

// RefreshTelegramMetadata accepts only the trusted host's private sender.
// Matching the existing Telegram binding and monotonic update guards retries.
func (s Service) RefreshTelegramMetadata(ctx context.Context, owner string, update TelegramMetadataUpdate) error {
	if update.UpdateID < 0 || update.UpdateID == math.MaxInt64 || !ValidTelegramMetadata(update.Sender) {
		return nil
	}
	sender := update.Sender
	printName := sender.FirstName
	if sender.LastName != "" {
		printName += " " + sender.LastName
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	changed, err := tx.Exec(ctx, `UPDATE core.users SET username=$3,first_name=$4,last_name=$5,print_name=$6,name=$6,
 telegram_metadata_update=$7 WHERE id=$1 AND telegram_id=$2 AND telegram_metadata_update<$7`,
		owner, sender.ID, sender.Username, sender.FirstName, sender.LastName, printName, update.UpdateID)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if changed.RowsAffected() > 0 {
		if err = broadcastprofile.Telegram(ctx, tx, owner, broadcastprofile.Sender{
			ID: sender.ID, Username: sender.Username, FirstName: sender.FirstName,
			LastName: sender.LastName, PrintName: printName,
		}); err != nil {
			return err
		}
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func ValidTelegramMetadata(sender SenderMetadata) bool {
	const maxNameRunes = 256
	const maxUsernameBytes = 64
	if sender.IsBot || strings.TrimSpace(sender.FirstName) == "" ||
		!validTelegramName(sender.FirstName, maxNameRunes) ||
		!validTelegramName(sender.LastName, maxNameRunes) ||
		len(sender.Username) > maxUsernameBytes {
		return false
	}
	for _, char := range sender.Username {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func validTelegramName(value string, limit int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) || char == '\ufffe' || char == '\uffff' {
			return false
		}
	}
	return true
}
