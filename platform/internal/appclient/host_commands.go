package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Host) validateUser(ctx context.Context, owner string) error {
	if c.UserToken == nil {
		return identity.ErrZitadelIdentity
	}
	_, err := c.UserToken(ctx, owner)
	return err
}
func (c Host) CheckReadAuthorities(ctx context.Context, owner string, authorities []readsource.Authority) error {
	if c.LocalHistory != nil {
		return hostHistoryWrite(ctx, c, owner, func(s conversation.Service, actor string) error {
			if !readsource.Valid(authorities) {
				return &core.ProblemError{Status: http.StatusBadRequest, Code: invalidJSONCode}
			}
			return s.CheckReadAuthorities(ctx, actor, authorities)
		})
	}
	if err := c.validateUser(ctx, owner); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		ReadAuthorities []readsource.Authority `json:"read_authorities"`
	}{authorities})
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
		"/internal/history/authority",
		body,
		&result,
	)
}
func (c Host) ClaimAdminMessage(ctx context.Context) (adminmessage.Delivery, bool, error) {
	var result struct {
		Delivery adminmessage.Delivery `json:"delivery"`
		Found    bool                  `json:"found"`
	}
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/claim",
		nil,
		&result,
	)
	return result.Delivery, result.Found, err
}
func (c Host) CompleteAdminMessage(ctx context.Context, result adminmessage.Completion) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var out struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/complete",
		body,
		&out,
	)
}

func (c Host) BeginAdminMessage(ctx context.Context, input delivery.Attempt) (delivery.Admission, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return delivery.Admission{}, err
	}
	var out delivery.Admission
	err = c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost, "/internal/admin-messages/begin", body, &out)
	if err != nil {
		return delivery.Admission{}, err
	}
	return out, nil
}
