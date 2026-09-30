package passbooking

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

type adminDatabasePaymentTracer struct {
	failureContext   context.Context
	closeConnection  bool
	secondaryQueries int
	proofQueries     int
	proofError       error
	proofRows        int64
	closeError       error
	faultPrefix      string
	faultQueries     int
	abortTransaction bool
	abortError       error
	lastError        error
}

func (tracer *adminDatabasePaymentTracer) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT p.kind,p.id") {
		tracer.secondaryQueries++
	}
	prefix := tracer.faultPrefix
	if prefix == "" {
		prefix = "SELECT p.kind,p.id"
	}
	if strings.HasPrefix(data.SQL, prefix) {
		tracer.faultQueries++
		if tracer.abortTransaction {
			_, tracer.abortError = conn.Exec(ctx, "BEGIN; SELECT 1/0")
		}
		if tracer.closeConnection {
			tracer.closeError = conn.Close(ctx)
		}
		if tracer.failureContext != nil {
			return tracer.failureContext
		}
	}
	return ctx
}

func (tracer *adminDatabasePaymentTracer) TraceQueryEnd(
	_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData,
) {
	tracer.lastError = data.Err
	if tracer.secondaryQueries == 0 {
		tracer.proofQueries++
		tracer.proofError, tracer.proofRows = data.Err, data.CommandTag.RowsAffected()
	}
}

func TestAdminDatabasePaymentProofFallbackFailures(t *testing.T) {
	t.Parallel()
	config := adminDatabasePaymentConfig(t)
	for _, stage := range []struct {
		name   string
		prefix string
		actor  string
	}{
		{name: "legacy_visitor", prefix: "SELECT COALESCE(m.receiving_admin", actor: "visitor"},
		{name: "legacy_owner", prefix: "SELECT COALESCE(m.receiving_admin", actor: "owner"},
		{name: "absence_owner", prefix: "-- name: HasUnsubmittedPayment", actor: "owner"},
	} {
		t.Run(stage.name, func(t *testing.T) {
			t.Parallel()
			for _, fault := range []string{"transport", "postgres", "denied"} {
				t.Run(fault, func(t *testing.T) {
					t.Parallel()
					tracer := &adminDatabasePaymentTracer{
						faultPrefix:      stage.prefix,
						closeConnection:  fault == "transport",
						abortTransaction: fault == "postgres",
					}
					cfg := config.Copy()
					cfg.ConnConfig.Tracer = tracer
					db, err := pgxpool.NewWithConfig(t.Context(), cfg)
					require.NoError(t, err)
					t.Cleanup(db.Close)
					_, err = (Service{DB: db}).PaymentProof(t.Context(), stage.actor, "missing", "owner")
					require.Equal(t, 1, tracer.proofQueries)
					require.NoError(t, tracer.proofError)
					require.Zero(t, tracer.proofRows)
					require.Equal(t, 1, tracer.secondaryQueries)
					require.Equal(t, 1, tracer.faultQueries, "fault must reach the selected fallback query")
					require.NoError(t, tracer.closeError)
					if fault == "postgres" {
						var setup, driver *pgconn.PgError
						require.ErrorAs(t, tracer.abortError, &setup)
						require.Equal(t, "22012", setup.Code)
						require.ErrorAs(t, tracer.lastError, &driver)
						require.Equal(t, "25P02", driver.Code)
					}
					if fault == "denied" {
						require.Equal(t, forbidden(), err)
					} else {
						require.Equal(t, core.ErrDatabase, err, "driver details must be sanitized")
					}
				})
			}
		})
	}
}

func adminDatabasePaymentConfig(t *testing.T) *pgxpool.Config {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_payment_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, store.Migrate(t.Context(), db))
	return config
}

func TestAdminDatabasePaymentProofSecondaryFailure(t *testing.T) {
	t.Parallel()
	config := adminDatabasePaymentConfig(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, expire := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer expire()
	for _, test := range []struct {
		name   string
		tracer adminDatabasePaymentTracer
		want   error
	}{
		{name: "database", tracer: adminDatabasePaymentTracer{closeConnection: true}, want: core.ErrDatabase},
		{name: "cancelled", tracer: adminDatabasePaymentTracer{failureContext: cancelled}, want: context.Canceled},
		{name: "deadline", tracer: adminDatabasePaymentTracer{failureContext: expired}, want: context.DeadlineExceeded},
		{name: "denied", want: forbidden()},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Copy()
			cfg.ConnConfig.Tracer = &test.tracer
			db, err := pgxpool.NewWithConfig(t.Context(), cfg)
			require.NoError(t, err)
			t.Cleanup(db.Close)
			_, err = (Service{DB: db}).PaymentProof(t.Context(), "visitor", "missing", "owner")
			require.Equal(t, 1, test.tracer.proofQueries, "first proof lookup must execute successfully without rows")
			require.NoError(t, test.tracer.proofError)
			require.Zero(t, test.tracer.proofRows)
			require.Equal(t, 1, test.tracer.secondaryQueries, "fault must occur in the secondary payment lookup")
			require.NoError(t, test.tracer.closeError)
			require.Equal(t, test.want, err)
		})
	}
}
