package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func redactionRequest(t *testing.T, f *fixture, target int64) botdelivery.CardRequest {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.pass_views(owner,chat_id,revision,message_id,state)
 VALUES('alice',101,5,$1,'{"redacted":true,"source":{"generation":0,"authorities":[],"private_history":true}}')`, target)
	require.NoError(t, err)
	return botdelivery.CardRequest{
		Owner: "alice", Chat: 101, Target: target,
		Reference: botdelivery.Reference{
			Kind: botdelivery.CardIntent, Family: "pass_redaction", CardKey: "passes", Revision: 5,
			Continuation: botdelivery.Continuation{Kind: "pass_redaction", Revision: 5},
		},
	}
}

func TestPassRedactionRejectsForgedAuthority(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"valid", "nonredacted", "revision", "foreign_chat", "source", "generation", "authorities", "zero_target", "missing_target", "card_key", "extra_field", "continuation"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			in := redactionRequest(t, f, 123)
			generation := int64(0)
			switch scenario {
			case "nonredacted":
				_, err := f.db.Exec(
					t.Context(),
					`UPDATE bot.pass_views SET state=jsonb_set(state,'{redacted}','false') WHERE owner='alice'`,
				)
				require.NoError(t, err)
			case "revision":
				in.Reference.Revision++
				in.Reference.Continuation.Revision++
			case "foreign_chat":
				in.Chat = 202
			case "source":
				in.Reference.Source = &readsource.Derivation{
					Generation: &generation, Authorities: []readsource.Authority{}, PrivateHistory: true,
				}
			case "generation":
				in.Reference.Generation = &generation
			case "authorities":
				in.Reference.Authorities = []readsource.Authority{
					{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
				}
			case "zero_target":
				in.Target = 0
			case "missing_target":
				_, err := f.db.Exec(t.Context(), `UPDATE bot.pass_views SET message_id=0 WHERE owner='alice'`)
				require.NoError(t, err)
			case "card_key":
				in.Reference.CardKey = "foreign"
			case "extra_field":
				in.Reference.Event = "unrelated"
			case "continuation":
				in.Reference.Continuation.Revision++
			}
			require.True(t, in.Reference.Valid(in.Owner), "exercise authority rejection after structural validation")
			err := f.b.Host.EnqueueBotCard(t.Context(), in)
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				requireCode(t, err, botdelivery.ErrStale.Code)
			}
			var intents int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents`).Scan(&intents))
			if scenario == "valid" {
				require.Equal(t, 1, intents, "the identical unforged fixture must admit an intent")
			} else {
				require.Zero(t, intents)
				pumpBotDeliveries(t, f.b)
			}
			require.Empty(t, chatMessages(t, f, 101))
			require.Empty(t, chatMessages(t, f, 202))
		})
	}
}

func TestPassRedactionQueuedTargetAndRevision(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"foreign_target", "revision", "target_gone"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			own, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "private obsolete card"})
			require.NoError(t, err)
			foreign, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 202, Text: "foreign card"})
			require.NoError(t, err)
			in := redactionRequest(t, f, own.ID)
			if scenario == "foreign_target" {
				require.NotEqual(t, own.ID, foreign.ID)
				in.Target = foreign.ID
				requireCode(t, f.b.Host.EnqueueBotCard(t.Context(), in), botdelivery.ErrStale.Code)
				var intents int
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents`).Scan(&intents),
				)
				require.Zero(t, intents)
				pumpBotDeliveries(t, f.b)
				require.Equal(t, []telegram.Message{own}, chatMessages(t, f, 101))
				require.Equal(t, []telegram.Message{foreign}, chatMessages(t, f, 202))
				return
			}
			require.NoError(t, f.b.Host.EnqueueBotCard(t.Context(), in))
			if scenario == "revision" {
				_, err = f.db.Exec(t.Context(), `UPDATE bot.pass_views SET revision=revision+1 WHERE owner='alice'`)
				require.NoError(t, err)
			}
			edits, sends := 0, 0
			f.b.TG.HTTP = redactionTargetHTTP(scenario, &edits, &sends)
			pumpBotDeliveries(t, f.b)
			require.Zero(t, sends, "redaction must never become a new send")
			var state delivery.Kind
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state FROM bot.delivery_intents WHERE reference->>'family'='pass_redaction'`).
					Scan(&state),
			)
			switch scenario {
			case "revision":
				require.Equal(t, delivery.Cancelled, state)
				require.Zero(t, edits, "a changed view must cancel before transport")
			case "target_gone":
				require.Equal(t, delivery.Rejected, state)
				require.Equal(t, 1, edits)
			}
			foreignMessages := chatMessages(t, f, 202)
			require.Len(t, foreignMessages, 1)
			require.Equal(t, foreign.ID, foreignMessages[0].ID)
			require.Equal(t, foreign.Text, foreignMessages[0].Text)
		})
	}
}

func redactionTargetHTTP(scenario string, edits, sends *int) *http.Client {
	return &http.Client{Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/sendMessage") {
			*sends++
		}
		if strings.HasSuffix(request.URL.Path, "/editMessageText") {
			*edits++
			if scenario == "target_gone" {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header:     make(http.Header),
					Body: io.NopCloser(
						strings.NewReader(`{"ok":false,"error_code":400,"description":"message to edit not found"}`),
					),
				}, nil
			}
		}
		return http.DefaultTransport.RoundTrip(request)
	})}
}

// A deferred edit waits for the persisted deadline; the next attempt still edits
// only the fixed unavailable notice at the canonical tombstone target.
type redactionRetryTransport struct {
	payloads     []telegram.Send
	missing      bool
	edits, sends int
}

func (r *redactionRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/sendMessage") {
		r.sends++
	}
	if !strings.HasSuffix(request.URL.Path, "/editMessageText") {
		return http.DefaultTransport.RoundTrip(request)
	}
	r.edits++
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if err = request.Body.Close(); err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var payload telegram.Send
	if err = json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	r.payloads = append(r.payloads, payload)
	if r.edits == 1 {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body: io.NopCloser(
				strings.NewReader(
					`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`,
				),
			),
		}, nil
	}
	if r.missing {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body: io.NopCloser(
				strings.NewReader(`{"ok":false,"error_code":400,"description":"message to edit not found"}`),
			),
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestPassRedactionRetriesAfterActualDeadline(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"sent", "target_gone", "revision_changed", "target_changed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			own, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "private obsolete card"})
			require.NoError(t, err)
			in := redactionRequest(t, f, own.ID)
			archive := conversation.Service{DB: f.db}
			require.NoError(
				t,
				archive.AppendOriginal(t.Context(), "alice", "redaction-retry-source", "user", "private source"),
			)
			deletePassDeliveryHistory(t, f, "alice")
			var saved botdelivery.PassMenu
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state FROM bot.pass_views WHERE owner='alice'`).Scan(&saved),
			)
			require.True(t, saved.Redacted)
			requireCode(t, f.b.Host.CheckBotDeliverySource(t.Context(), botdelivery.SourceRequest{
				Owner: "alice", Source: saved.Source,
			}), "history_stale")
			require.NoError(t, f.b.Host.EnqueueBotCard(t.Context(), in))
			notice, err := i18n.Translate("ru", i18n.RegistrationUnavailable, nil)
			require.NoError(t, err)
			wire := &redactionRetryTransport{missing: scenario == "target_gone"}
			f.b.TG.HTTP = &http.Client{Transport: wire}
			pumpBotDeliveries(t, f.b)
			var state delivery.Kind
			var attempt int64
			var deferred bool
			require.NoError(t, f.db.QueryRow(
				t.Context(),
				`SELECT state,attempt,not_before>clock_timestamp() FROM bot.delivery_intents WHERE reference->>'family'='pass_redaction'`,
			).Scan(&state, &attempt, &deferred))
			require.Equal(t, delivery.Deferred, state)
			require.EqualValues(t, 1, attempt)
			require.True(t, deferred, "the provider cooldown must be persisted against the database clock")
			require.Equal(t, 1, wire.edits)
			require.Zero(t, wire.sends)
			if scenario == "revision_changed" {
				_, err = f.db.Exec(t.Context(), `UPDATE bot.pass_views SET revision=revision+1 WHERE owner='alice'`)
				require.NoError(t, err)
			}
			if scenario == "target_changed" {
				_, err = f.db.Exec(t.Context(), `UPDATE bot.pass_views SET message_id=message_id+1 WHERE owner='alice'`)
				require.NoError(t, err)
			}
			require.Eventually(t, func() bool {
				return len(botDeliveryCandidates(t, f.b)) > 0
			}, 5*time.Second, 10*time.Millisecond, "wait for actual queue eligibility without changing its deadline")
			pumpBotDeliveries(t, f.b)
			require.NoError(t, f.db.QueryRow(
				t.Context(),
				`SELECT state,attempt FROM bot.delivery_intents WHERE reference->>'family'='pass_redaction'`,
			).Scan(&state, &attempt))
			if scenario == "revision_changed" || scenario == "target_changed" {
				require.Equal(t, delivery.Cancelled, state)
				require.EqualValues(t, 1, attempt)
				require.Equal(t, 1, wire.edits)
			} else {
				require.EqualValues(t, 2, attempt)
				require.Equal(t, 2, wire.edits)
			}
			require.Zero(t, wire.sends)
			for _, payload := range wire.payloads {
				require.Equal(t, int64(101), payload.ChatID)
				require.Equal(t, own.ID, payload.MessageID)
				require.Equal(t, notice, payload.Text)
				require.Empty(t, payload.Markup.Rows)
			}
			switch scenario {
			case "sent":
				require.Equal(t, delivery.Succeeded, state)
				messages := chatMessages(t, f, 101)
				require.Len(t, messages, 1)
				require.Equal(t, own.ID, messages[0].ID)
				require.Equal(t, notice, messages[0].Text)
				require.Empty(t, messages[0].Markup.Rows)
			case "target_gone":
				require.Equal(t, delivery.Rejected, state)
			}
			var after botdelivery.PassMenu
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state FROM bot.pass_views WHERE owner='alice'`).Scan(&after),
			)
			require.Equal(t, saved, after, "delivery cannot replace the revoked private source or tombstone")
		})
	}
}
