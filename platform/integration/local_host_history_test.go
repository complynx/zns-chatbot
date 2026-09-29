package integration_test

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func hostHistoryBoundary(t *testing.T, s conversation.Service, transport string) (appclient.Client, appclient.Host) {
	t.Helper()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	handler := api.AuthenticatedHandler(
		appservices.Services{Core: core.Service{DB: s.DB}, Conversation: s},
		signer,
		slog.New(slog.DiscardHandler),
		verify,
	)
	var requests atomic.Int64
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); handler.ServeHTTP(w, r) }),
	)
	t.Cleanup(server.Close)
	client := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	host := appclient.Host{Base: server.URL, HTTP: server.Client(), Signer: signer, UserToken: client.UserToken}
	if transport == "local" {
		local := &appclient.LocalHistory{Service: s, Authorizer: applicationauth.Authorizer{DB: s.DB, Verify: verify}}
		client.LocalHistory, host.LocalHistory = local, local
		t.Cleanup(func() { require.Zero(t, requests.Load(), "local history must not call HTTP") })
	}
	return client, host
}

func TestLocalHostHistoryArchiveReadsAndSummary(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"local", "http"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			service := conversation.Service{DB: database(t)}
			client, host := hostHistoryBoundary(t, service, transport)
			ctx := t.Context()
			original := strings.Repeat("Привет 🌍 ", 900)
			require.NoError(t, host.ArchiveOriginal(ctx, "alice", "tg-user-1", "user", original))
			require.NoError(t, host.ArchiveOriginal(ctx, "alice", "tg-user-1", "user", "replay must not replace body"))
			require.NoError(t, host.ArchiveOriginal(ctx, "bob", "tg-user-2", "manual", "Bob private message"))
			require.NoError(t, host.ArchiveOutcome(ctx, "alice", "notice-1", "Committed outcome"))
			require.NoError(
				t,
				host.ArchiveDerived(
					ctx,
					"alice",
					"tg-assistant-1",
					"Derived answer",
					1,
					false,
					0,
					[]readsource.Authority{},
				),
			)
			page, err := client.ConversationHistory(ctx, "alice", conversation.Query{Limit: 2})
			require.NoError(t, err)
			require.Len(t, page.Events, 2)
			require.True(t, page.More)
			require.Equal(t, "Derived answer", page.Events[0].Text)
			older, err := client.ConversationHistory(
				ctx,
				"alice",
				conversation.Query{Before: page.NextBefore, Limit: 2},
			)
			require.NoError(t, err)
			require.Len(t, older.Events, 1)
			require.True(t, older.Events[0].HasFullText)
			var body strings.Builder
			offset, digest := 0, ""
			for {
				chunk, readErr := client.ConversationText(
					ctx,
					"alice",
					older.Events[0].ID,
					offset,
					conversation.MaxChunkCharacters,
					digest,
				)
				require.NoError(t, readErr)
				body.WriteString(chunk.Text)
				if !chunk.More {
					break
				}
				offset, digest = chunk.NextOffset, chunk.Digest
			}
			require.Equal(t, original, body.String())
			var bobID int64
			require.NoError(
				t,
				service.DB.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='bob'`).Scan(&bobID),
			)
			_, err = client.ConversationText(ctx, "alice", bobID, 0, 1, "")
			requireCode(t, err, "history_missing")
			window, err := client.ConversationWindow(ctx, "alice", 3)
			require.NoError(t, err)
			require.Len(t, window.Recent, 3)
			require.Equal(t, older.Events[0].ID, window.Recent[0].ID)
			require.NoError(t, client.CheckHistoryGeneration(ctx, "alice", window.Generation))
			require.NoError(t, host.CheckReadAuthorities(ctx, "alice", []readsource.Authority{}))
			batch, err := host.HistorySummaryBatch(ctx, "alice", math.MaxInt64)
			require.NoError(t, err)
			require.Len(t, batch, 2, "full-body originals are not eligible for lossy summaries")
			ids := []int64{batch[0].ID, batch[1].ID}
			require.NoError(
				t,
				host.CommitHistorySummary(ctx, "alice", window.Summary.Version, ids, "Earlier outcome and answer."),
			)
			require.Error(t, host.CommitHistorySummary(ctx, "alice", window.Summary.Version, ids, "Stale replacement."))
			require.Error(t, host.CommitHistorySummary(ctx, "bob", 0, ids, "Cross-owner coverage."))
			restarted, _ := hostHistoryBoundary(t, conversation.Service{DB: service.DB}, transport)
			window, err = restarted.ConversationWindow(ctx, "alice", 3)
			require.NoError(t, err)
			require.EqualValues(t, 1, window.Summary.Version)
			require.Equal(t, "Earlier outcome and answer.", window.Summary.Text)
			requireCode(t, host.ArchiveOriginal(ctx, "alice", "forged", "assistant", "Model original"), "invalid_json")
			_, err = host.HistorySummaryBatch(ctx, "alice", -1)
			requireCode(t, err, "invalid_json")
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			_, err = client.ConversationWindow(canceled, "alice", 1)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, host.ArchiveOutcome(canceled, "alice", "canceled", "Must not persist"), context.Canceled)
			var count int
			require.NoError(
				t,
				service.DB.QueryRow(ctx, `SELECT count(*) FROM core.conversation_events WHERE source_key=ANY($1)`, []string{"forged", "canceled"}).
					Scan(&count),
			)
			require.Zero(t, count)
		})
	}
}

func TestLocalHostHistoryRedactionAndRevocation(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"local", "http"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			service, authority := conversationAuthorityFixture(t)
			client, host := hostHistoryBoundary(t, service, transport)
			ctx := t.Context()
			require.NoError(
				t,
				host.ArchiveOriginal(ctx, "alice", "tg-user-3", "user", "My passport is SECRET-DOCUMENT"),
			)
			require.NoError(
				t,
				host.ArchiveDerived(
					ctx,
					"alice",
					"tg-assistant-3",
					"SECRET-DOCUMENT reply",
					3,
					false,
					0,
					[]readsource.Authority{},
				),
			)
			require.NoError(
				t,
				host.ArchiveDerived(
					ctx,
					"alice",
					"tg-assistant-4",
					"https://private-media.invalid/token",
					4,
					true,
					0,
					[]readsource.Authority{},
				),
			)
			page, err := client.ConversationHistory(ctx, "alice", conversation.Query{Limit: 2})
			require.NoError(t, err)
			require.Equal(t, "[media response; expiring content omitted]", page.Events[0].Text)
			require.Equal(t, "[response to sensitive request omitted]", page.Events[1].Text)
			refs := readsource.Registration([]passbooking.ReadAuthority{authority})
			require.NoError(t, host.CheckReadAuthorities(ctx, "alice", refs))
			require.NoError(
				t,
				host.ArchiveDerived(
					ctx,
					"alice",
					"tg-assistant-5",
					"Authorized private booking detail",
					5,
					false,
					0,
					refs,
				),
			)
			page, err = client.ConversationHistory(ctx, "alice", conversation.Query{Limit: 1})
			require.NoError(t, err)
			require.Equal(t, refs, page.Events[0].ReadAuthorities)
			derivedID := page.Events[0].ID
			_, err = service.DB.Exec(ctx, `DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'`)
			require.NoError(t, err)
			requireCode(t, host.CheckReadAuthorities(ctx, "alice", refs), "history_stale")
			window, err := client.ConversationWindow(ctx, "alice", 1)
			require.NoError(t, err)
			require.True(t, window.Recent[0].Omitted)
			require.Positive(t, window.Generation)
			require.ErrorIs(t, client.CheckHistoryGeneration(ctx, "alice", 0), appclient.ErrReadStale)
			chunk, err := client.ConversationText(ctx, "alice", derivedID, 0, 100, "")
			require.NoError(t, err)
			require.True(t, chunk.Omitted)
			require.Empty(t, chunk.Text)
			requireCode(
				t,
				host.ArchiveDerived(ctx, "alice", "tg-assistant-6", "Stale reply", 6, false, 0, refs),
				"history_stale",
			)
			var count int
			require.NoError(
				t,
				service.DB.QueryRow(ctx, `SELECT count(*) FROM core.conversation_events WHERE source_key='tg-assistant-6'`).
					Scan(&count),
			)
			require.Zero(t, count)
		})
	}
}

func TestLocalHostHistoryResourceBudgets(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"local", "http"} {
		for _, test := range []struct{ name, query, text string }{
			{"details", `UPDATE core.conversation_events SET details=jsonb_build_object('body',$1::text)`, strings.Repeat("x", core.ReadResourceBytes+1)},
			{"aggregate", `INSERT INTO core.conversation_events(owner,source_key,kind,details) SELECT 'alice','extra-'||n,'system',jsonb_build_object('body',$1::text) FROM generate_series(1,19) n`, strings.Repeat("x", 60000)},
			{"escaping", `UPDATE core.conversation_events SET details=jsonb_build_object('body',$1::text)`, strings.Repeat("<", 200000)},
			{"omission reason", `UPDATE core.conversation_events SET omission_reason=$1`, strings.Repeat("x", core.ReadResourceBytes+1)},
			{"authorities", `INSERT INTO core.conversation_read_authorities(event_id,authorities,history_generation) SELECT id,jsonb_build_array(jsonb_build_object('body',$1::text)),0 FROM core.conversation_events`, strings.Repeat("x", core.ReadResourceBytes+1)},
		} {
			t.Run(transport+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				service := conversation.Service{DB: database(t)}
				client, host := hostHistoryBoundary(t, service, transport)
				require.NoError(t, service.AppendOriginal(t.Context(), "alice", "seed", "user", "Readable body"))
				_, err := service.DB.Exec(t.Context(), test.query, test.text)
				require.NoError(t, err)
				page, err := client.ConversationHistory(t.Context(), "alice", conversation.Query{Limit: 20})
				requireCode(t, err, "read_result_limit")
				require.Empty(t, page)
				window, err := client.ConversationWindow(t.Context(), "alice", 30)
				requireCode(t, err, "read_result_limit")
				require.Empty(t, window)
				batch, err := host.HistorySummaryBatch(t.Context(), "alice", math.MaxInt64)
				requireCode(t, err, "read_result_limit")
				require.Empty(t, batch)
				if test.name == "details" {
					var id int64
					require.NoError(
						t,
						service.DB.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='seed'`).
							Scan(&id),
					)
					chunk, readErr := client.ConversationText(t.Context(), "alice", id, 0, 100, "")
					require.NoError(t, readErr)
					require.Equal(t, "Readable body", chunk.Text, "unread metadata must not block a bounded text chunk")
				}
			})
		}
	}
}

func TestLocalHostHistorySummaryAuthorityBudget(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"local", "http"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			service := conversation.Service{DB: database(t)}
			client, _ := hostHistoryBoundary(t, service, transport)
			require.NoError(t, service.AppendOriginal(t.Context(), "alice", "seed", "user", "Text"))
			_, err := service.DB.Exec(
				t.Context(),
				`UPDATE core.conversation_summaries SET read_authorities=jsonb_build_array(jsonb_build_object('body',$1::text)) WHERE owner='alice'`,
				strings.Repeat("x", core.ReadResourceBytes+1),
			)
			require.NoError(t, err)
			value, err := client.ConversationWindow(t.Context(), "alice", 1)
			requireCode(t, err, "read_result_limit")
			require.Empty(t, value)
		})
	}
}
