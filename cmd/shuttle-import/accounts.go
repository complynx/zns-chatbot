package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

const (
	accountPlanLimit       = 1 << 20
	accountCredentialLimit = 16384
	accountImportTimeout   = 15 * time.Minute
)

type accountImportPlan struct {
	OperatorUserID uuid.UUID                 `json:"operator_user_id"`
	Request        db.HistoryAccountsRequest `json:"request"`
}

type accountImportIdentity interface {
	Verify(context.Context, string) (identity.Principal, error)
	ReadProfileName(context.Context, identity.Account, int64) (identity.ProfileName, error)
}

// readImportFile rejects links and limits private operator input without logging it.
func readImportFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("invalid private import file")
	}
	f, err := os.Open(path) // #nosec G304 -- explicit operator-selected file.
	if err != nil {
		return nil, errors.New("cannot open private import file")
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("private import file changed")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("private import file exceeds limit")
	}
	return raw, nil
}

func parseAccountPlan(raw []byte) (accountImportPlan, error) {
	var plan accountImportPlan
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil || plan.OperatorUserID == uuid.Nil {
		return accountImportPlan{}, errors.New("invalid account import plan")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return accountImportPlan{}, errors.New("account import plan must contain one document")
	}
	return plan, nil
}

func accountIdentityOptions(c config.Config) identity.Options {
	i := c.Identity
	return identity.Options{
		Issuer: i.Issuer, OrganizationID: i.OrganizationID, BotID: c.Telegram.BotID,
		LoomAudience: i.Audience, APIAudience: i.APIAudience, ActorID: i.ActorID,
		ActorClientCredentials:    identity.ClientCredentials{ID: i.ActorClientID, Secret: i.ActorClientSecret},
		ExchangeClientCredentials: identity.ClientCredentials{ID: i.ClientID, Secret: i.ClientSecret},
		IntrospectionClientCredentials: identity.ClientCredentials{
			ID: i.IntrospectionClientID, Secret: i.IntrospectionClientSecret,
		},
		PositiveCacheTTL: 0,
	}
}

func authorizeAccountImport(ctx context.Context, iam accountImportIdentity, token string, scope db.ProfileScope) error {
	p, err := iam.Verify(ctx, token)
	if err != nil || p.Subject != scope.Subject || p.OrganizationID != scope.Organization {
		return identity.ErrDenied
	}
	if _, ok := p.Roles["zinusya.admin"][scope.Organization]; !ok {
		return identity.ErrDenied
	}
	return nil
}

// verifyAccountBindings reads the exact current IAM accounts. Profile names are
// discarded: technical provisioning names must not become confirmed app names.
func verifyAccountBindings(ctx context.Context, iam accountImportIdentity, token string,
	scope db.ProfileScope, bindings []db.HistoryAccountBinding) error {
	if err := authorizeAccountImport(ctx, iam, token, scope); err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.BotID != scope.BotID || binding.Organization != scope.Organization {
			return identity.ErrDenied
		}
		account := identity.Account{
			Subject: binding.Subject, OrganizationID: binding.Organization,
			SyntheticEmail: "tg+" + strconv.FormatInt(binding.BotID, 10) + "+" +
				strconv.FormatInt(binding.TelegramID, 10) + "@telegram.invalid",
		}
		if _, err := iam.ReadProfileName(ctx, account, binding.TelegramID); err != nil {
			return err
		}
	}
	return authorizeAccountImport(ctx, iam, token, scope)
}

func runAccounts(args []string, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("apply-accounts", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Existing application configuration")
	planPath := flags.String("plan", "", "Private archive-to-IAM account mapping")
	credentialPath := flags.String("authorization-file", "", "Private current application-admin JWT file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		*configPath == "" || *planPath == "" || *credentialPath == "" {
		_, _ = fmt.Fprintln(diagnostics,
			"usage: shuttle-import apply-accounts -config app.yaml -plan plan.json -authorization-file admin.jwt")
		return exitInvalidInput
	}
	fail := func() int {
		_, _ = fmt.Fprintln(
			diagnostics,
			"Account import failed; inspect private receipts before retrying the same plan.",
		)
		return 1
	}
	raw, err := readImportFile(*planPath, accountPlanLimit)
	if err != nil {
		return fail()
	}
	plan, err := parseAccountPlan(raw)
	if err != nil {
		return fail()
	}
	raw, err = readImportFile(*credentialPath, accountCredentialLimit)
	if err != nil {
		return fail()
	}
	token := strings.TrimSpace(string(raw))
	c, err := config.Load(*configPath)
	if err != nil {
		return fail()
	}
	iam, err := identity.New(accountIdentityOptions(c), &http.Client{Timeout: c.HTTPTimeout})
	if err != nil {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountImportTimeout)
	defer cancel()
	principal, err := iam.Verify(ctx, token)
	if err != nil {
		return fail()
	}
	scope := db.ProfileScope{BotID: c.Telegram.BotID, Organization: c.Identity.OrganizationID,
		Subject: principal.Subject, UserID: plan.OperatorUserID}
	if err = authorizeAccountImport(ctx, iam, token, scope); err != nil {
		return fail()
	}
	store, err := db.Open(ctx, c.DatabaseURL)
	if err != nil {
		return fail()
	}
	defer store.Close()
	if err = store.BindDeployment(ctx, c.Telegram.BotID, c.Identity.OrganizationID, c.Identity.Issuer); err != nil {
		return fail()
	}
	receipts, err := store.ImportHistoryAccounts(ctx, scope, plan.Request,
		func(ctx context.Context, bindings []db.HistoryAccountBinding) error {
			return verifyAccountBindings(ctx, iam, token, scope, bindings)
		})
	if err != nil {
		return fail()
	}
	// Per-user receipts remain private in PostgreSQL. Console output contains counts.
	if err = json.NewEncoder(output).Encode(struct {
		Accounts int `json:"accounts"`
	}{Accounts: len(receipts)}); err != nil {
		return fail()
	}
	return 0
}
