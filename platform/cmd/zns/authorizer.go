package main

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func configureAuthorizerProvisioning(
	ctx context.Context,
	next http.Handler,
	service identityprovision.Service,
	sdk *identityprovision.SDK,
	cfg config.Config,
) (http.Handler, error) {
	a := cfg.Auth.Zitadel.Provisioning.Authorizer
	if a == (config.Authorizer{}) {
		return next, nil
	}
	verifier, err := identity.NewAuthorizer(
		ctx,
		identity.AuthorizerConfig{
			Issuer:         a.Issuer,
			JWKSURL:        a.JWKSURL,
			BotID:          service.BotID,
			AllowLocalHTTP: cfg.Env == sandboxMode,
		},
	)
	if err != nil {
		return nil, err
	}
	linker := identityprovision.ExternalLinker{Provisioner: service, Provider: sdk, IDP: a.IDPID}
	return api.WithAuthorizerProvisioning(next, verifier, linker), nil
}
