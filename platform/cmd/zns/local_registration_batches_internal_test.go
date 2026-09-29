package main

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestCombinedClientInjectsRegistrationBatches(t *testing.T) {
	t.Parallel()
	batchDB := &pgxpool.Pool{}
	client := combinedClient(
		"http://127.0.0.1:1",
		nil,
		appservices.Services{DerivedMutations: derivedmutation.Service{DB: batchDB}},
		applicationauth.Authorizer{},
	)
	require.NotNil(t, client.LocalRegistration)
	require.Same(t, batchDB, client.LocalRegistration.Batches.DB)
}

type combinedBatchTransport func(*http.Request) (*http.Response, error)

func (f combinedBatchTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCombinedClientRunsRegistrationBatchPostgres(t *testing.T) {
	t.Parallel()
	db := combinedBatchDatabase(t)
	_, err := db.Exec(t.Context(), `
 INSERT INTO core.pass_events(id,finishes_at) VALUES('combined-batch',now()+interval '30 days');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('combined-batch',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('combined-batch','bob');
 INSERT INTO core.pass_booking_admins(owner) VALUES('bob');
 INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader');`)
	require.NoError(t, err)
	services := appservices.NewServices(db, appservices.Options{})
	initial, err := services.Registration.Execute(t.Context(), "alice", passbooking.Command{
		Key: "combined-seed", Event: "combined-batch", Name: "solo", PaymentAdmin: "bob",
	})
	require.NoError(t, err)
	require.Equal(t, "assigned", initial.State)
	signer := identity.Signer{Key: []byte(strings.Repeat("c", 32))}
	authorizer := applicationauth.Authorizer{DB: db, Verify: func(_ context.Context, token string) (string, error) {
		return signer.Verify(token)
	}}
	var requests atomic.Int32
	transport := &http.Client{Transport: combinedBatchTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("combined registration must not use HTTP")
	})}
	client := combinedClient("http://local-only.invalid", transport, services, authorizer)
	client.SandboxToken = signer.Token
	require.NotNil(t, client.LocalRegistration)
	require.Same(t, db, client.LocalRegistration.Service.DB)
	require.Same(t, db, client.LocalRegistration.Batches.DB)
	items, err := client.RunPassBatch(t.Context(), "bob", passbooking.RuntimeBatch{
		Key: "combined-real-batch", Event: "combined-batch", Action: "admin_cancel", Recipients: []int64{101},
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	require.Zero(t, requests.Load(), "the actual combined client must execute locally")
	current, err := services.Registration.Get(t.Context(), "alice", "combined-batch")
	require.NoError(t, err)
	require.Equal(t, "cancelled", current.State)
	require.Greater(t, current.Version, initial.Version)
}

func combinedBatchDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_combined_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	return db
}
