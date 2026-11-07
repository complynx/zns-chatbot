package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

type importIdentityProbe struct {
	principal identity.Principal
	verified  []identity.Account
	checks    int
	revokeAt  int
}

func (p *importIdentityProbe) Verify(context.Context, string) (identity.Principal, error) {
	p.checks++
	if p.revokeAt > 0 && p.checks >= p.revokeAt {
		return identity.Principal{}, identity.ErrDenied
	}
	return p.principal, nil
}

func (p *importIdentityProbe) ReadProfileName(
	_ context.Context, account identity.Account, _ int64,
) (identity.ProfileName, error) {
	p.verified = append(p.verified, account)
	return identity.ProfileName{FirstName: "Technical", LastName: "Placeholder"}, nil
}

func TestAccountImportChecksCurrentAuthorityAndExactBindings(t *testing.T) {
	t.Parallel()
	scope := db.ProfileScope{BotID: 123, Organization: "org", Subject: "operator", UserID: uuid.New()}
	bindings := []db.HistoryAccountBinding{{BotID: 123, TelegramID: 42, Organization: "org", Subject: "target"}}
	probe := &importIdentityProbe{principal: identity.Principal{Subject: "operator", OrganizationID: "org",
		Roles: map[string]map[string]string{"zinusya.admin": {"org": "Organization"}}}}
	require.NoError(t, verifyAccountBindings(context.Background(), probe, "private", scope, bindings))
	require.Equal(t, []identity.Account{{Subject: "target", OrganizationID: "org",
		SyntheticEmail: "tg+123+42@telegram.invalid"}}, probe.verified)
	require.Equal(t, 2, probe.checks, "authority must be current after the target reads")
	probe.checks, probe.revokeAt = 0, 2
	require.ErrorIs(
		t,
		verifyAccountBindings(context.Background(), probe, "private", scope, bindings),
		identity.ErrDenied,
	)
}

func TestAccountImportRejectsForeignAuthorityBeforeTargetReads(t *testing.T) {
	t.Parallel()
	scope := db.ProfileScope{BotID: 123, Organization: "org", Subject: "operator", UserID: uuid.New()}
	for _, principal := range []identity.Principal{
		{Subject: "stranger", OrganizationID: "org", Roles: map[string]map[string]string{"zinusya.admin": {"org": "Org"}}},
		{Subject: "operator", OrganizationID: "other", Roles: map[string]map[string]string{"zinusya.admin": {"org": "Org"}}},
		{
			Subject:        "operator",
			OrganizationID: "org",
			Roles:          map[string]map[string]string{"zinusya.admin": {"other": "Other"}},
		},
	} {
		probe := &importIdentityProbe{principal: principal}
		require.ErrorIs(t, verifyAccountBindings(
			context.Background(),
			probe,
			"private",
			scope,
			[]db.HistoryAccountBinding{
				{BotID: 123, TelegramID: 42, Organization: "org", Subject: "target"},
			},
		), identity.ErrDenied)
		require.Empty(t, probe.verified)
	}
}

func TestAccountImportPlanRejectsUnknownOrTrailingInput(t *testing.T) {
	t.Parallel()
	plan := accountImportPlan{OperatorUserID: uuid.New(), Request: db.HistoryAccountsRequest{ArchiveID: uuid.New()}}
	raw, err := json.Marshal(plan)
	require.NoError(t, err)
	parsed, err := parseAccountPlan(raw)
	require.NoError(t, err)
	require.Equal(t, plan, parsed)
	_, err = parseAccountPlan(append(raw, []byte(" {}")...))
	require.Error(t, err)
	_, err = parseAccountPlan([]byte(`{"operator_user_id":"` + plan.OperatorUserID.String() + `","unknown":"private"}`))
	require.Error(t, err)
}

func TestAccountImportArgumentErrorsDoNotExposePrivateInputs(t *testing.T) {
	t.Parallel()
	var output, diagnostics bytes.Buffer
	require.Equal(
		t,
		exitInvalidInput,
		run([]string{"apply-accounts", "-private-secret", "DO_NOT_PRINT"}, &output, &diagnostics),
	)
	require.Empty(t, output.String())
	require.NotContains(t, diagnostics.String(), "DO_NOT_PRINT")
}
