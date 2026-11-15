package main

import (
	"context"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/identity"
)

//nolint:nilnil // A nil source disables optional personalization without disabling literal delivery.
func personalizationToken(c config.Config, h *http.Client) (func(context.Context) (identity.Credential, error), error) {
	p := c.Loom.Personalization
	if p.Subject == "" {
		return nil, nil
	}
	machine, err := identity.NewMachineTokenSource(identity.MachineOptions{
		Issuer: c.Identity.Issuer, OrganizationID: c.Identity.OrganizationID,
		Audience: c.Identity.Audience, Subject: p.Subject,
		Credentials: identity.ClientCredentials{ID: p.ClientID, Secret: p.ClientSecret},
	}, h)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (identity.Credential, error) {
		return machine.Token(ctx, time.Minute)
	}, nil
}
