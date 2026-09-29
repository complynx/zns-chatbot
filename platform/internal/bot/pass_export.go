package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (c APIClient) ExportPasses(ctx context.Context, owner string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/v1/passes/export", http.NoBody)
	if err != nil {
		return nil, err
	}
	token, err := c.userToken(ctx, owner)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.httpClient()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("core API unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem core.ProblemError
		if err = json.NewDecoder(io.LimitReader(response.Body, maxAPIBytes)).Decode(&problem); err != nil {
			return nil, err
		}
		problem.Status = response.StatusCode
		return nil, &problem
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, passbooking.MaxExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > passbooking.MaxExportBytes {
		return nil, errors.New("invalid export size")
	}
	return body, nil
}

func (b *Bot) exportPasses(ctx context.Context, in incoming, update int64) (i18n.ID, error) {
	var sent bool
	err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='pass_export')`, in.owner, update).
		Scan(&sent)
	if err != nil {
		return "", err
	}
	if sent {
		return i18n.RegistrationExported, nil
	}
	body, err := b.API.ExportPasses(ctx, in.owner)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return i18n.RegistrationExportUnavailable, nil
	}
	if err != nil {
		return "", err
	}
	message, err := b.TG.SendDocument(ctx, in.chat, "passes.xlsx", body)
	if err != nil {
		return "", err
	}
	err = b.record(
		ctx,
		in.owner,
		update,
		"pass_export",
		map[string]any{"message_id": message.ID, "filename": "passes.xlsx"},
	)
	return i18n.RegistrationExported, err
}

func (b *Bot) handlePassExport(ctx context.Context, in incoming, update telegram.Update) error {
	notice, err := b.exportPasses(ctx, in, update.ID)
	if err != nil {
		return err
	}
	state, _, err := b.passMenuState(ctx, in.owner)
	if err != nil {
		return err
	}
	state.Notice = notice
	if err = b.storePassMenu(ctx, in.owner, in.chat, update.ID, state); err != nil {
		return err
	}
	return b.RenderPassMenu(ctx, in.owner, in.chat, notice)
}
