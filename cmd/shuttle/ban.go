package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/complynx/zns-chatbot/internal/db"
)

// runBanTelegramUser uses the configured local database only. Print a receipt
// only after a confirmed commit; a retry with the same arguments recovers it.
func runBanTelegramUser(
	ctx context.Context,
	store *db.Store,
	botID, telegramUserID int64,
	actor, reason string,
	output io.Writer,
) error {
	result, err := store.BanTelegramUser(ctx, botID, telegramUserID, actor, reason)
	if err != nil {
		return fmt.Errorf("ban local Telegram user: %w", err)
	}
	return json.NewEncoder(output).Encode(result)
}
