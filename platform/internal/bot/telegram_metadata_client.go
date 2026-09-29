package bot

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (c APIClient) RefreshTelegramMetadata(ctx context.Context, owner string, input core.TelegramMetadataUpdate) error {
	if _, err := c.userToken(ctx, owner); err != nil {
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
