package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	host := appclient.Host{
		Base:   f.b.Host.Base,
		Signer: signer,
		UserToken: func(ctx context.Context, owner string) (string, error) {
			if txErr := boundaryNoOpenTX(ctx, f.db); txErr != nil {
				return "", txErr
			}
			return signer.Token(owner), nil
		},
	}
	if local {
		f.b.API.LocalHistory = &appclient.LocalHistory{
			Service: services.Conversation,
			Authorizer: applicationauth.Authorizer{
				DB:     db,
				Verify: func(_ context.Context, token string) (string, error) { return signer.Verify(token) },
			},
		}
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
			for _, q := range []string{`SELECT name FROM core.users LIMIT 1`, `SELECT text FROM core.conversation_summaries LIMIT 1`, `UPDATE core.knowledge_memos SET body=body WHERE false`} {
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
				_, restoreErr := f.db.Exec(
					t.Context(),
					`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
				)
				require.NoError(t, restoreErr)
				require.NoError(t, f.b.DeliverBotIntent(t.Context(), i.QueueReference()))
				require.Zero(t, wire.calls.Load())
			}
		})
	}
}

func TestBotDeliveryBoundaryGenerationWithRestrictedRole(t *testing.T) {
	t.Parallel()
	for _, local := range []bool{false, true} {
		for _, kind := range []botdelivery.Kind{botdelivery.CardIntent, botdelivery.DocumentIntent} {
			for _, stale := range []bool{false, true} {
				t.Run(fmt.Sprintf("local_%t/%s/stale_%t", local, kind, stale), func(t *testing.T) {
					t.Parallel()
					runBotDeliveryGenerationBoundary(t, local, kind, stale)
				})
			}
		}
	}
}

func runBotDeliveryGenerationBoundary(t *testing.T, local bool, kind botdelivery.Kind, stale bool) {
	t.Helper()
	f := memorySplitRoleFixture(t)
	f.b.Host = boundaryRoleHost(t, f, local)
	_, err := f.b.DB.Exec(
		t.Context(),
		`UPDATE core.conversation_history_generations SET generation=generation WHERE false`,
	)
	var denied *pgconn.PgError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "42501", denied.Code)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.conversation_history_generations(owner,generation) VALUES('alice',7);
INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING`,
	)
	require.NoError(t, err)
	wire := boundaryGenerationTransport(t, f)
	if kind == botdelivery.CardIntent {
		require.NoError(t, f.b.RenderProfile(t.Context(), "alice", identity.AliceTelegramID))
	} else {
		handle(t, f.b, message(42001, identity.AliceTelegramID, "/get_file opaque_id"))
	}
	ref := delivery.Reference{Owner: delivery.Bot}
	require.NoError(t, f.b.DB.QueryRow(t.Context(),
		`SELECT operation_key,effect_key FROM bot.delivery_intents WHERE owner='alice' AND reference->>'kind'=$1`,
		string(kind)).Scan(&ref.Key, &ref.Effect))
	queued, readErr := botdelivery.Read(t.Context(), f.b.DB, f.b.Delivery.BotID, ref, false)
	require.NoError(t, readErr)
	require.NotNil(t, queued.Reference.Generation)
	require.EqualValues(t, 7, *queued.Reference.Generation)
	require.Nil(t, queued.Reference.Source, "exercise generation binding without a derived source")
	require.Zero(t, wire.calls.Load(), "enqueue must not send to Telegram")
	if stale {
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE core.conversation_history_generations SET generation=8 WHERE owner='alice'`,
		)
		require.NoError(t, err)
	}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
	done, doneErr := botdelivery.Read(t.Context(), f.b.DB, f.b.Delivery.BotID, ref, false)
	require.NoError(t, doneErr)
	require.EqualValues(t, 7, *done.Reference.Generation, "dispatch must preserve the captured generation")
	if stale {
		require.Equal(t, delivery.Cancelled, done.State)
		require.Zero(t, wire.calls.Load(), "deleted history must prevent private transport")
	} else {
		require.Equal(t, delivery.Succeeded, done.State)
		require.Positive(t, done.MessageID)
		require.True(t, done.ContinuationDone)
		require.Positive(t, wire.calls.Load())
	}
	calls := wire.calls.Load()
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
	require.Equal(t, calls, wire.calls.Load(), "terminal effects must never resend")
}

func boundaryGenerationTransport(t *testing.T, f *fixture) *boundaryTransport {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /botsynthetic/getFile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).
			Encode(map[string]any{"ok": true, "result": telegram.File{Path: "documents/original.pdf", Size: 9}})
	})
	mux.HandleFunc("GET /file/botsynthetic/documents/original.pdf", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("synthetic"))
	})
	mux.HandleFunc("POST /botsynthetic/sendDocument", func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("document")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, readErr := io.ReadAll(file)
		if readErr != nil || string(body) != "synthetic" || header.Filename != "opaque_id.pdf" ||
			r.FormValue("chat_id") != "101" {
			t.Errorf(
				"unexpected document: filename=%q chat=%q body=%q error=%v",
				header.Filename,
				r.FormValue("chat_id"),
				body,
				readErr,
			)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	mux.HandleFunc("POST /botsynthetic/sendMessage", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	wire := &boundaryTransport{}
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: &http.Client{Transport: wire}}
	return wire
}
