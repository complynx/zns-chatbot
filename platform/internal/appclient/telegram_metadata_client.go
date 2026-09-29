package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/account"
)

func (c Host) RefreshTelegramMetadata(ctx context.Context, owner string, input account.TelegramMetadataUpdate) error {
	if err := c.validateUser(ctx, owner); err != nil {
		return err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.MemoryProvenanceToken(owner),
		http.MethodPost,
		"/internal/telegram/metadata",
		data,
		&result,
	)
}
