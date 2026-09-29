package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"golang.org/x/oauth2"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

const identityUsersCommand = "users"

func executePrepareIdentities(arguments []string) (result, error) {
	if len(arguments) == 0 || arguments[0] != identityUsersCommand {
		return result{}, errors.New("expected_prepare_identities_users")
	}
	flags := flag.NewFlagSet("prepare-identities users", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	input := migrate.IdentityPreparation{Limits: migrate.DefaultLimits()}
	flags.StringVar(&input.Stage, "stage", "", "verified staging directory")
	flags.StringVar(&input.Plan, "plan", "", "reviewed users plan")
	flags.StringVar(&input.Policy, "policy", "", "reviewed per-user eligibility")
	flags.StringVar(&input.Output, "out", "", "private complete resolutions file")
	registerLimits(flags, &input.Limits)
	if flags.Parse(arguments[1:]) != nil || flags.NArg() != 0 || input.Stage == "" || input.Plan == "" ||
		input.Policy == "" || input.Output == "" {
		return result{}, errors.New("invalid_arguments")
	}
	input.DatabaseURL = os.Getenv("MIGRATE_DATABASE_URL")
	input.Issuer = os.Getenv("MIGRATE_IDENTITY_ISSUER")
	input.Organization = os.Getenv("MIGRATE_IDENTITY_ORGANIZATION")
	input.EmailDomain = os.Getenv("MIGRATE_IDENTITY_EMAIL_DOMAIN")
	token := os.Getenv("MIGRATE_IDENTITY_TOKEN")
	if input.DatabaseURL == "" || token == "" {
		return result{}, errors.New("identity_credentials_required")
	}
	provider, err := identityprovision.NewSDK(identityprovision.SDKConfig{Issuer: input.Issuer,
		TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"})})
	if err != nil {
		return result{}, errors.New("identity_configuration_invalid")
	}
	defer func() { _ = provider.Close() }()
	input.Provider = provider
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	summary, err := migrate.PrepareUserIdentities(ctx, input)
	return result{PrepareIdentities: &summary, Reused: summary.Reused}, err
}
