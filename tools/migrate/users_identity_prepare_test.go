package migrate_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

type preparationProvider struct {
	mu         sync.Mutex
	accounts   map[string]identityprovision.Account
	creates    int
	failCreate int
}

func (p *preparationProvider) Get(_ context.Context, id string) (identityprovision.Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	account, found := p.accounts[id]
	if !found {
		return account, identityprovision.ErrNotFound
	}
	return account, nil
}

func (p *preparationProvider) Create(_ context.Context, input identityprovision.Creation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creates++
	if p.creates == p.failCreate {
		return errors.New("PRIVATE-provider-token-name")
	}
	p.accounts[input.Subject] = identityprovision.Account{Subject: input.Subject, Organization: input.Organization,
		Operation: input.Operation, Active: true, Human: true}
	// A remote commit can succeed while the response is lost.
	return errors.New("PRIVATE-lost-response")
}

func identityInputs(t *testing.T) migrate.IdentityPreparation {
	t.Helper()
	stage, plan, _ := applyInputs(t,
		`{"_id":"first","bot_id":77,"user_id":101,"print_name":"First","first_name":"Anna","language_code":"en"}`,
		`{"_id":"second","bot_id":77,"user_id":102,"print_name":"Second","first_name":"Boris"}`,
		`{"_id":"excluded","bot_id":88,"user_id":103}`)
	data, err := os.ReadFile(plan)
	require.NoError(t, err)
	// Existing plan summary exposes the exact artifact hash without interpreting identities.
	summary, err := migrate.PlanUsers(stage, plan, migrate.DefaultLimits())
	require.NoError(t, err)
	require.NotEmpty(t, data)
	policy := migrate.IdentityPolicy{Version: 1, PlanSHA256: summary.ArtifactSHA256, Reviewed: true}
	for index, row := range userRows(t, plan) {
		if row.Status != "candidate" {
			continue
		}
		allowed := index == 0
		policy.Users = append(policy.Users, migrate.IdentityPolicyUser{LegacyKey: row.Legacy.Key, CanBook: &allowed})
	}
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	require.NoError(t, os.WriteFile(policyPath, raw, 0o600))
	return migrate.IdentityPreparation{Stage: stage, Plan: plan, Policy: policyPath,
		Output: filepath.Join(t.TempDir(), "resolutions.json"), Issuer: "https://identity.synthetic.invalid",
		Organization: "synthetic-org", EmailDomain: "migration.invalid", Limits: migrate.DefaultLimits(),
		Provider: &preparationProvider{accounts: map[string]identityprovision.Account{}}}
}

func TestIdentityPreparationResumesAndImportsReviewedPolicy(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	input := identityInputs(t)
	input.DatabaseURL = dsn
	provider := input.Provider.(*preparationProvider)
	provider.failCreate = 2
	summary, err := migrate.PrepareUserIdentities(t.Context(), input)
	require.EqualError(t, err, "identity_preparation_unavailable")
	assert.Equal(t, 1, summary.Prepared)
	_, err = os.Stat(input.Output)
	require.ErrorIs(t, err, os.ErrNotExist)
	var users, reservations int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.users`).Scan(&users))
	assert.Zero(t, users)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.identity_provisioning`).Scan(&reservations))
	assert.Equal(t, 2, reservations)
	summary, err = migrate.PrepareUserIdentities(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Prepared)
	assert.Equal(t, 3, provider.creates)
	assert.EqualValues(t, 1, summary.Excluded)
	raw, err := os.ReadFile(input.Output)
	require.NoError(t, err)
	var mappings migrate.UserResolutions
	require.NoError(t, json.Unmarshal(raw, &mappings))
	require.Len(t, mappings.Users, 2)
	assert.NotEqual(t, "101", mappings.Users[0].Subject)
	assert.True(t, *mappings.Users[0].CanBook)
	assert.False(t, *mappings.Users[1].CanBook)
	summary, err = migrate.PrepareUserIdentities(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, summary.Reused)
	assert.Equal(t, 3, provider.creates)
	applied, err := migrate.ApplyUsers(t.Context(), dsn, input.Stage, input.Plan, input.Output, input.Limits)
	require.NoError(t, err)
	assert.EqualValues(t, 2, applied.Applied)
	var allowed bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT can_book FROM core.users WHERE telegram_id=102`).Scan(&allowed))
	assert.False(t, allowed)
	summary, err = migrate.PrepareUserIdentities(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, summary.Reused)
	assert.Equal(t, 3, provider.creates)
	applied, err = migrate.ApplyUsers(t.Context(), dsn, input.Stage, input.Plan, input.Output, input.Limits)
	require.NoError(t, err)
	assert.EqualValues(t, 2, applied.Reused)
}

func TestIdentityPreparationRejectsCompletePolicyBeforeProvider(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"version":1,"plan_sha256":"wrong","reviewed":true,"users":[]}`,
		`{"version":1,"reviewed":false,"users":[]}`,
		`{"version":1,"version":1,"reviewed":true,"users":[]}`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			input := identityInputs(t)
			input.DatabaseURL = "PRIVATE-invalid-dsn"
			require.NoError(t, os.WriteFile(input.Policy, []byte(body), 0o600))
			_, err := migrate.PrepareUserIdentities(t.Context(), input)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "PRIVATE")
			assert.Zero(t, input.Provider.(*preparationProvider).creates)
		})
	}
}

func TestIdentityPreparationConcurrentAndRemoteConflict(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	input := identityInputs(t)
	input.DatabaseURL = dsn
	other := input
	other.Output = filepath.Join(t.TempDir(), "other.json")
	results := make(chan error, 2)
	for _, candidate := range []migrate.IdentityPreparation{input, other} {
		go func() {
			_, err := migrate.PrepareUserIdentities(t.Context(), candidate)
			results <- err
		}()
	}
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	left, err := os.ReadFile(input.Output)
	require.NoError(t, err)
	right, err := os.ReadFile(other.Output)
	require.NoError(t, err)
	assert.Equal(t, left, right)
	provider := input.Provider.(*preparationProvider)
	assert.Equal(t, 2, provider.creates)
	for key, account := range provider.accounts {
		account.Operation = "different-operation"
		provider.accounts[key] = account
	}
	_, err = migrate.PrepareUserIdentities(t.Context(), input)
	require.EqualError(t, err, "identity_preparation_conflict")
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.users`).Scan(&count))
	assert.Zero(t, count)
}

func TestIdentityPreparationDoesNotBypassAtomicApply(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	input := identityInputs(t)
	input.DatabaseURL = dsn
	_, err := migrate.PrepareUserIdentities(t.Context(), input)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`ALTER TABLE core.admin_broadcast_profiles ADD CONSTRAINT reject_test_profile CHECK(false)`,
	)
	require.NoError(t, err)
	_, err = migrate.ApplyUsers(t.Context(), dsn, input.Stage, input.Plan, input.Output, input.Limits)
	require.Error(t, err)
	for _, table := range []string{"core.users", "core.pass_profiles", "core.telegram_identities", "core.zitadel_identities", "migrate_import.user_receipts"} {
		var count int
		require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		assert.Zero(t, count, table)
	}
	_, err = db.Exec(t.Context(), `ALTER TABLE core.admin_broadcast_profiles DROP CONSTRAINT reject_test_profile`)
	require.NoError(t, err)
	applied, err := migrate.ApplyUsers(t.Context(), dsn, input.Stage, input.Plan, input.Output, input.Limits)
	require.NoError(t, err)
	assert.EqualValues(t, 2, applied.Applied)
}
