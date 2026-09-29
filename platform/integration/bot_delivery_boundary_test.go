package integration_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const boundaryApplication = "synthetic_bot_delivery_boundary"

type boundaryTransport struct {
	before func(*http.Request) error
	calls  atomic.Int64
}

func (t *boundaryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.before != nil {
		if err := t.before(r); err != nil {
			return nil, err
		}
	}
	t.calls.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}
func boundaryRoleHost(t *testing.T, f *fixture, local bool) appclient.Host {
	t.Helper()
	// Reproduce sandbox/roles.sql application ownership in this isolated database.
	_, err := f.db.Exec(
		t.Context(),
		`GRANT USAGE ON SCHEMA core TO zns_api;GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA core TO zns_api;GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA core TO zns_api`,
	)
	require.NoError(t, err)
	cfg := f.db.Config()
	cfg.ConnConfig.RuntimeParams["application_name"] = boundaryApplication
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error { _, e := c.Exec(ctx, "SET ROLE zns_api"); return e }
	db, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	services := appservices.NewServices(
		db,
		appservices.Options{Delivery: syntheticDeliverySettings(), LegacyOrderBotID: 77},
	)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	host := appclient.Host{Signer: signer, UserToken: func(ctx context.Context, owner string) (string, error) {
		if err := boundaryNoOpenTX(ctx, f.db); err != nil {
			return "", err
		}
		return signer.Token(owner), nil
	}}
	if local {
		host.LocalBotDelivery = &appclient.LocalBotDelivery{
			Service: services.BotDelivery,
			Authorizer: applicationauth.Authorizer{
				DB:     db,
				Verify: func(_ context.Context, token string) (string, error) { return signer.Verify(token) },
			},
		}
	} else {
		server := httptest.NewServer(api.Handler(services, signer, slog.New(slog.DiscardHandler)))
		t.Cleanup(server.Close)
		host.Base = server.URL
	}
	return host
}
func boundaryNoOpenTX(ctx context.Context, db *pgxpool.Pool) error {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND xact_start IS NOT NULL`, boundaryApplication).
		Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("external call inside %d application transactions", n)
	}
	return nil
}
func boundaryResult(effect string) botdelivery.ResultRequest {
	return botdelivery.ResultRequest{
		Owner:     "alice",
		Chat:      identity.AliceTelegramID,
		Update:    41000,
		Effect:    effect,
		Reference: botdelivery.Reference{Family: "static"},
		Result:    botdelivery.StoredResult{Payload: telegram.Send{Text: "synthetic private delivery canary"}},
	}
}
func boundaryIntent(t *testing.T, f *fixture, in botdelivery.ResultRequest) botdelivery.Intent {
	t.Helper()
	key, effect := botdelivery.ResultOperation(in.Owner, in.Update, in.Effect)
	i, err := botdelivery.Read(
		t.Context(),
		f.b.DB,
		f.b.Delivery.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: key, Effect: effect},
		false,
	)
	require.NoError(t, err)
	return i
}
func TestBotDeliveryBoundarySplitRolesAndRestart(t *testing.T) {
	t.Parallel()
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local_%t", local), func(t *testing.T) {
			t.Parallel()
			f := memorySplitRoleFixture(t)
			host := boundaryRoleHost(t, f, local)
			f.b.Host = host
			wire := &boundaryTransport{
				before: func(r *http.Request) error { return boundaryNoOpenTX(r.Context(), f.db) },
			}
			f.b.TG.HTTP = &http.Client{Transport: wire}
			for _, q := range []string{`SELECT telegram_id FROM core.users LIMIT 1`, `SELECT version FROM core.conversation_summaries LIMIT 1`, `UPDATE core.knowledge_memos SET body=body WHERE false`} {
				_, err := f.b.DB.Exec(t.Context(), q)
				var denied *pgconn.PgError
				require.ErrorAs(t, err, &denied)
				require.Equal(t, "42501", denied.Code)
			}
			in := boundaryResult("restart")
			wrong := host
			wrong.UserToken = func(context.Context, string) (string, error) { return host.Signer.Token("bob"), nil }
			require.Error(t, wrong.EnqueueBotResult(t.Context(), in))
			require.NoError(t, host.EnqueueBotResult(t.Context(), in))
			i := boundaryIntent(t, f, in)
			forged := i
			forged.Chat++
			_, err := host.BeginBotDelivery(t.Context(), botdelivery.BeginRequest{Observed: forged})
			require.Error(t, err)
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), i.QueueReference()))
			done := boundaryIntent(t, f, in)
			require.Equal(t, delivery.Succeeded, done.State)
			require.Positive(t, done.MessageID)
			require.True(t, done.ContinuationDone)
			require.Equal(t, int64(1), wire.calls.Load())
			in.Result.Payload.Text = "replacement must never be persisted"
			require.NoError(t, host.EnqueueBotResult(t.Context(), in))
			restarted := *f.b
			restarted.Host = boundaryRoleHost(t, f, local)
			require.NoError(t, restarted.DeliverBotIntent(t.Context(), i.QueueReference()))
			require.Equal(t, int64(1), wire.calls.Load())
			var body botdelivery.StoredResult
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, in.Owner, in.Update, i.Reference.ResultKind).
					Scan(&body),
			)
			require.Equal(t, "synthetic private delivery canary", body.Payload.Text)
			stale := done
			stale.Attempt++
			require.Error(t, host.ApplyBotDeliveryReceipt(t.Context(), botdelivery.ReceiptRequest{Observed: stale}))
		})
	}
}
func TestBotDeliveryBoundaryRevocationAfterRender(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"history", "grant", "recipient"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := memorySplitRoleFixture(t)
			host := boundaryRoleHost(t, f, false)
			f.b.Host = host
			in := boundaryResult(kind)
			generation := int64(0)
			source := &readsource.Derivation{
				Generation:     &generation,
				Authorities:    []readsource.Authority{},
				PrivateHistory: true,
			}
			if kind == "grant" {
				_, err := f.db.Exec(
					t.Context(),
					`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
				)
				require.NoError(t, err)
				source.Authorities = []readsource.Authority{
					{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
				}
			}
			in.Reference.Source = source
			in.Result.Source = source
			require.NoError(t, host.EnqueueBotResult(t.Context(), in))
			i := boundaryIntent(t, f, in)
			var intercepted atomic.Bool
			barrier := &boundaryTransport{before: func(r *http.Request) error {
				if r.URL.Path != "/internal/bot-delivery/begin" || intercepted.Swap(true) {
					return nil
				}
				var err error
				switch kind {
				case "history":
					_, err = f.db.Exec(
						r.Context(),
						`INSERT INTO core.conversation_history_generations(owner,generation) VALUES('alice',1) ON CONFLICT(owner) DO UPDATE SET generation=1`,
					)
				case "grant":
					_, err = f.db.Exec(
						r.Context(),
						`DELETE FROM core.knowledge_permissions WHERE actor='alice' AND permission='review'`,
					)
				case "recipient":
					_, err = f.db.Exec(r.Context(), `UPDATE core.users SET telegram_id=999001 WHERE id='alice'`)
				}
				return err
			}}
			f.b.Host.HTTP = &http.Client{Transport: barrier}
			wire := &boundaryTransport{}
			f.b.TG.HTTP = &http.Client{Transport: wire}
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), i.QueueReference()))
			require.True(t, intercepted.Load(), "proof must cross render before authority loss")
			retired := boundaryIntent(t, f, in)
			require.Equal(t, delivery.Cancelled, retired.State)
			require.Zero(t, retired.Attempt)
			require.Zero(t, wire.calls.Load())
			if kind == "grant" {
				_, err := f.db.Exec(
					t.Context(),
					`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
				)
				require.NoError(t, err)
				require.NoError(t, f.b.DeliverBotIntent(t.Context(), i.QueueReference()))
				require.Zero(t, wire.calls.Load())
			}
		})
	}
}
