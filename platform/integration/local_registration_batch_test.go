package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func registrationBatchClients(
	t *testing.T,
	db *pgxpool.Pool,
	service derivedmutation.Service,
) (appclient.Client, appclient.Client) {
	t.Helper()
	signer := identity.Signer{Key: []byte("registration-batches-test-key-32bytes")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	authorizer := applicationauth.Authorizer{DB: db, Verify: verify}
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.Services{
				Core:             core.Service{DB: db},
				Registration:     service.Registration,
				DerivedMutations: service,
			},
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{
		SandboxToken: signer.Token,
		LocalRegistration: &appclient.LocalRegistration{
			Service:    service.Registration,
			Batches:    service,
			Authorizer: authorizer,
		},
	}
	return local, remote
}

func TestLocalRegistrationBatchStoredSource(t *testing.T) {
	t.Parallel()
	db, registration := adminPairFixture(t)
	service := derivedmutation.Service{DB: db, Registration: registration}
	local, remote := registrationBatchClients(t, db, service)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review');
 CREATE FUNCTION core.interrupt_second_boundary_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.plan->'items'->1->'outcome'->>'status' <> 'not_attempted' THEN RAISE EXCEPTION 'synthetic second marker interruption'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER interrupt_second_boundary_item BEFORE UPDATE ON core.pass_admin_batches FOR EACH ROW EXECUTE FUNCTION core.interrupt_second_boundary_item()`,
	)
	require.NoError(t, err)
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
	tier := 1
	command := passbooking.RuntimeBatch{
		Key:        "local-batch-source",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101, 202},
		Options:    passbooking.AdminAssignment{AppendTier: &tier},
	}
	_, err = service.RunPassBatch(t.Context(), "bob", command, source)
	require.ErrorContains(t, err, "synthetic second marker interruption")
	var original string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_derivation::text FROM core.pass_admin_batches`).Scan(&original),
	)
	_, err = db.Exec(
		t.Context(),
		`DROP TRIGGER interrupt_second_boundary_item ON core.pass_admin_batches; DELETE FROM core.knowledge_permissions WHERE actor='bob'`,
	)
	require.NoError(t, err)
	items, err := local.RunPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	require.Equal(t, passbooking.AdminBatchRejected, items[1].Outcome.Status)
	require.Equal(t, "source_stale", items[1].Outcome.Code)
	replay, err := remote.RunPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Equal(t, items, replay)
	var persisted string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_derivation::text FROM core.pass_admin_batches`).Scan(&persisted),
	)
	require.Equal(t, original, persisted)
	var amount int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	require.Equal(t, 22, amount)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.RunPassBatch(t.Context(), "bob", command)
		requireCode(t, err, "forbidden")
	}
}

func TestLocalRegistrationBatchManualHTTPParity(t *testing.T) {
	t.Parallel()
	db, registration := adminPairFixture(t)
	service := derivedmutation.Service{DB: db, Registration: registration}
	local, remote := registrationBatchClients(t, db, service)
	command := passbooking.RuntimeBatch{
		Key:        "local-batch-manual",
		Event:      "dance",
		Action:     "admin_cancel",
		Recipients: []int64{101},
	}
	items, err := local.RunPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Len(t, items, 1)
	replay, err := remote.RunPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Equal(t, items, replay)
	var sourceMissing bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_derivation IS NULL FROM core.pass_admin_batches`).Scan(&sourceMissing),
	)
	require.True(t, sourceMissing)
	for _, client := range []appclient.Client{local, remote} {
		_, readErr := client.RunPassBatch(t.Context(), "alice", command)
		requireCode(t, readErr, "forbidden")
		_, readErr = client.RunPassBatch(t.Context(), "bob", passbooking.RuntimeBatch{})
		requireCode(t, readErr, "pass_booking_invalid")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.RunPassBatch(ctx, "bob", command)
		require.ErrorIs(t, readErr, context.Canceled)
	}
}
