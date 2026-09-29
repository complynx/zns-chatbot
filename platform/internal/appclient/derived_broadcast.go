package appclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Host) PreviewDerivedAdminMessage(
	ctx context.Context,
	owner, key, command string,
	source readsource.Derivation,
) (adminmessage.Message, error) {
	var result adminmessage.Message
	err := c.derivedRequest(ctx, owner, "/internal/derived/admin-messages/preview", struct {
		Key     string `json:"key"`
		Command string `json:"command"`
	}{key, command}, source, &result)
	return result, err
}

func (c Host) BeginDerivedAdminMessageInput(
	ctx context.Context,
	owner, key, command string,
	chat int64,
	source readsource.Derivation,
) (adminmessage.Input, error) {
	var result adminmessage.Input
	err := c.derivedRequest(ctx, owner, "/internal/derived/admin-messages/input/start", struct {
		Key     string `json:"key"`
		Command string `json:"command"`
		ChatID  int64  `json:"chat_id"`
	}{key, command, chat}, source, &result)
	return result, err
}

func (c Host) AttachDerivedAdminMessageInput(
	ctx context.Context,
	owner string,
	command adminmessage.Attachment,
	source readsource.Derivation,
) (adminmessage.Message, error) {
	var result adminmessage.Message
	err := c.derivedRequest(ctx, owner, "/internal/derived/admin-messages/input/attach", command, source, &result)
	return result, err
}

func (c Host) CancelDerivedAdminMessageInput(
	ctx context.Context,
	owner string,
	id int64,
	source readsource.Derivation,
) error {
	var result struct {
		OK bool `json:"ok"`
	}
	return c.derivedRequest(ctx, owner, "/internal/derived/admin-messages/input/cancel", struct {
		ID int64 `json:"id"`
	}{id}, source, &result)
}

func (c Client) CheckAdminMessagePublication(ctx context.Context, owner string, id int64) error {
	var result struct {
		OK bool `json:"ok"`
	}
	return c.Call(ctx, owner, http.MethodPost, fmt.Sprintf("/v1/admin-messages/%d/publication", id), nil, &result)
}
