package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminMessageNativeIntakePreservesRegistrationClock(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := foodPendingDatabase(t)
	b := botDeliveryTestBot(db)
	first := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	b.RegistrationClock = passBatchClock{now: first}
	update := telegram.Update{ID: 601, Callback: &telegram.Callback{
		From: telegram.User{ID: 202}, Data: "adminmsg:results:42",
		Message: telegram.Message{Chat: telegram.Chat{ID: 202, Type: "private"}},
	}}
	offset, err := b.saveBatch(ctx, 0, []telegram.Update{update})
	require.NoError(t, err)
	require.Equal(t, int64(602), offset)
	readProof := func() {
		t.Helper()
		var received time.Time
		var owner, control string
		var generation int64
		require.NoError(t, db.QueryRow(ctx, `SELECT received_at,intake_owner,intake_generation,intake_control
 FROM core.registration_ingress WHERE bot_id=$1 AND request_key='601'`, b.Delivery.BotID).
			Scan(&received, &owner, &generation, &control))
		require.Equal(t, first, received.UTC())
		require.Equal(t, "bob", owner)
		require.Zero(t, generation)
		require.Equal(t, "adminmsg:results:42", control)
	}
	readProof()
	_, err = db.Exec(ctx, `INSERT INTO core.conversation_history_generations(owner,generation) VALUES('bob',1)
 ON CONFLICT(owner) DO UPDATE SET generation=1`)
	require.NoError(t, err)
	b.RegistrationClock = passBatchClock{now: first.Add(time.Hour)}
	update.Callback.Data = "adminmsg:results:43"
	_, err = b.saveBatch(ctx, 0, []telegram.Update{update})
	require.NoError(t, err)
	readProof()
	// A failed observation rolls back both intake proof and inbox acknowledgement.
	b.RegistrationClock = passBatchClock{err: context.Canceled}
	update.ID = 602
	offset, err = b.saveBatch(ctx, 602, []telegram.Update{update})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int64(602), offset)
	var exists bool
	require.NoError(t, db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.registration_ingress
 WHERE bot_id=$1 AND request_key='602') OR EXISTS(SELECT 1 FROM bot.telegram_inbox WHERE update_id=602)`,
		b.Delivery.BotID).Scan(&exists))
	require.False(t, exists)
	var savedOffset int64
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT value FROM bot.cursors WHERE name='telegram_received'`).Scan(&savedOffset),
	)
	require.Equal(t, int64(602), savedOffset)
}

func TestAdminMessageProgressEnglishAndRussian(t *testing.T) {
	t.Parallel()
	progress := adminmessage.JobProgress{
		Total:        55,
		Succeeded:    1,
		Queued:       2,
		Deferred:     3,
		Sending:      4,
		Rejected:     5,
		Cancelled:    6,
		Uncertain:    7,
		Parked:       8,
		Paused:       9,
		NotQueued:    10,
		SharedPaused: 2,
	}
	for _, test := range []struct{ language, prefix, uncertain, shared string }{
		{"en", "Whole broadcast:", "uncertain 7", "Shared service pause affects 2 queued/deferred recipients (included above)"},
		{"ru", "Вся рассылка:", "результат неизвестен 7", "Общая пауза доставки затрагивает 2 получателей в очереди или с отложенной попыткой (уже учтены выше)"},
	} {
		messages := orderMessages{language: test.language}
		text := adminMessageProgressText(progress, &messages)
		require.NoError(t, messages.err)
		require.True(t, strings.HasPrefix(text, test.prefix))
		require.Contains(t, text, test.uncertain)
		require.Contains(t, text, test.shared)
		require.NotContains(t, text, "{")
		for _, number := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
			require.Contains(t, text, number)
		}
		recovered := progress
		recovered.SharedPaused = 0
		text = adminMessageProgressText(recovered, &messages)
		require.NoError(t, messages.err)
		require.Contains(t, text, strings.Replace(test.shared, "2", "0", 1))
		require.NotContains(t, text, "{")
	}
}

func TestAdminMessageComposedPageDelivery(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, state := range []string{"cancelled", broadcastPreparing} {
			t.Run(language+"/"+state, func(t *testing.T) {
				t.Parallel()
				page := adminMessageLongTestPage(language, state)
				requests := make(chan adminPageTestResult, 100)
				errors := make(chan error, 100)
				server := httptest.NewServer(adminMessagePageTestHandler(page, requests, errors))
				defer server.Close()
				b := Bot{
					API: appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token},
					Host: appclient.Host{
						Base:      server.URL,
						UserToken: func(context.Context, string) (string, error) { return "synthetic", nil },
					},
				}
				in := incoming{owner: "admin", chat: 101}
				ctx := withAdminMessageSource(t.Context(), in, telegram.Update{ID: 99})
				messages := orderMessages{language: language}
				require.NoError(t, b.sendAdminMessagePage(ctx, in, page.ID, page.Offset, &messages))
				require.NoError(t, messages.err)
				firstCount := len(requests)
				// An unchanged response produces the same candidate effect keys and bodies.
				require.NoError(t, b.sendAdminMessagePage(ctx, in, page.ID, page.Offset, &messages))
				require.Len(t, requests, 2*firstCount)
				close(requests)
				close(errors)
				for err := range errors {
					require.NoError(t, err)
				}
				var saved []adminPageTestResult
				for request := range requests {
					saved = append(saved, request)
				}
				require.Equal(t, saved[:firstCount], saved[firstCount:])
				assertAdminMessageTestPage(t, page, saved[:firstCount], &messages)
			})
		}
	}
}

func TestAdminMessagePagePersistedReplayWithChangingLivePage(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	service := adminmessage.Service{DB: db, Delivery: botIntentTestSettings()}
	for _, language := range []string{"en", "ru"} {
		for _, state := range []string{"cancelled", broadcastPreparing} {
			for _, grow := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/grow=%t", language, state, grow), func(t *testing.T) {
					t.Parallel()
					assertAdminMessagePersistedReplay(t, service, language, state, grow)
				})
			}
		}
	}
}

// Field tags reflect the existing host request wire format without changing it.
type adminPageTestResult struct {
	Owner     string                   `json:"Owner"`
	Chat      int64                    `json:"Chat"`
	Update    int64                    `json:"Update"`
	Effect    string                   `json:"Effect"`
	Reference botdelivery.Reference    `json:"Reference"`
	Result    botdelivery.StoredResult `json:"Result"`
	Target    int64                    `json:"Target"`
}

func adminMessageLongTestPage(language, state string) adminmessage.Page {
	page := adminmessage.Page{ID: 42, State: state, Total: 60, Offset: 20, More: true,
		Progress: adminmessage.JobProgress{Total: 60, Cancelled: 60}}
	states := []string{
		"pending",
		"sending",
		"sent",
		"failed",
		"cancelled",
		"unknown",
		"parked",
		"paused",
		"draft",
		broadcastPreparing,
	}
	if state == "cancelled" {
		states = []string{"cancelled"}
	}
	failureLength := 100
	if language == "en" {
		failureLength = 200
	}
	for index := range 20 {
		page.Items = append(page.Items, adminmessage.Delivery{
			Destination: adminmessage.Destination{
				Chat:   fmt.Sprintf("@%s%02d", strings.Repeat("a", 30), index),
				Thread: 9223372036854775807,
			},
			State:   states[index%len(states)],
			Failure: strings.Repeat("x", failureLength),
			Content: adminmessage.Content{Text: "private reviewed content 🐈"},
		})
	}
	return page
}

func adminMessagePageTestHandler(
	page adminmessage.Page,
	requests chan<- adminPageTestResult,
	errors chan<- error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/admin-messages/review":
			errors <- json.NewEncoder(w).Encode(page)
		case "/v1/admin-messages/42/resume", "/v1/admin-messages/42/publication":
			errors <- json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case "/internal/bot-delivery/admin-page-results":
			var batch botdelivery.AdminPageResultsRequest
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				errors <- err
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, effect := range batch.Effects {
				if _, err := telegram.PrepareSend(effect.Payload); err != nil {
					errors <- err
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- adminPageTestResult{
					Owner: batch.Owner, Chat: batch.Chat, Update: batch.Update, Effect: effect.Effect,
					Reference: botdelivery.Reference{Family: botFamilyAdminPage, Version: batch.ID},
					Result:    botdelivery.StoredResult{Payload: effect.Payload},
				}
			}
			errors <- json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			errors <- fmt.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func assertAdminMessageTestPage(
	t *testing.T,
	page adminmessage.Page,
	requests []adminPageTestResult,
	messages *orderMessages,
) {
	t.Helper()
	var pageText, viewText strings.Builder
	var navigation telegram.Markup
	pageChunks := 0
	for _, request := range requests {
		require.Equal(t, "admin", request.Owner)
		require.Equal(t, int64(101), request.Chat)
		require.Equal(t, int64(99), request.Update)
		require.Equal(t, botFamilyAdminPage, request.Reference.Family)
		require.Equal(t, page.ID, request.Reference.Version)
		if !strings.HasPrefix(request.Effect, "admin_page:") {
			viewText.WriteString(request.Result.Payload.Text)
			continue
		}
		pageChunks++
		pageText.WriteString(request.Result.Payload.Text)
		if request.Effect == "admin_page:42:20" {
			navigation = request.Result.Payload.Markup
		} else {
			require.Empty(t, request.Result.Payload.Markup.Rows)
		}
	}
	require.Greater(t, pageChunks, 1)
	require.Greater(t, len(utf16.Encode([]rune(pageText.String()))), 4096)
	require.Contains(t, pageText.String(), adminMessageProgressText(page.Progress, messages))
	for _, item := range page.Items {
		require.Contains(t, pageText.String(), adminDestination(item.Destination))
	}
	for index := range 20 {
		require.Equal(t, fmt.Sprintf("adminmsg:inspect:42:%d", 20+index), navigation.Rows[index][0].Data)
	}
	require.Equal(t, "adminmsg:page:42:0", navigation.Rows[20][0].Data)
	require.Equal(t, "adminmsg:page:42:40", navigation.Rows[21][0].Data)
	if page.State == broadcastPreparing {
		require.Empty(t, viewText.String())
		require.Equal(t, "adminmsg:results:42", navigation.Rows[22][0].Data)
		require.Equal(t, "adminmsg:cancel:42", navigation.Rows[22][1].Data)
	} else {
		require.Equal(t, pageText.String()+"\n\n"+page.Items[0].Content.Text, viewText.String())
	}
}
func assertAdminMessagePersistedReplay(t *testing.T, service adminmessage.Service, language, state string, grow bool) {
	t.Helper()
	db := service.DB
	message, previewErr := service.Preview(t.Context(), "bob", t.Name(), adminmessage.Request{
		Destinations: []adminmessage.Destination{
			{Chat: "101"},
		},
		Content: adminmessage.Content{Text: "private content"},
	})
	require.NoError(t, previewErr)
	short := adminmessage.Page{
		ID:       message.ID,
		State:    state,
		Total:    60,
		Offset:   20,
		More:     true,
		Progress: adminmessage.JobProgress{Total: 60, Cancelled: 60},
		Items: []adminmessage.Delivery{
			{Destination: adminmessage.Destination{Chat: "101"}, State: "cancelled",
				Content: adminmessage.Content{Text: "original content"}},
			{Destination: adminmessage.Destination{Chat: "102"}, State: "cancelled",
				Content: adminmessage.Content{Text: "original content"}},
		},
	}
	long := short
	long.Items = append([]adminmessage.Delivery(nil), short.Items...)
	long.Items[0].Failure = strings.Repeat("x", 1000)
	long.Items[1].Failure = strings.Repeat("y", 1000)
	long.Items[0].Content.Text = "changed content"
	first, second := short, long
	if !grow {
		first, second = long, short
	}
	var live atomic.Value
	live.Store(first)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/admin-messages/review" {
			_ = json.NewEncoder(w).Encode(live.Load().(adminmessage.Page))
		} else {
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		}
	}))
	defer server.Close()
	b := botDeliveryTestBot(db)
	b.Host.LocalBotDelivery.Service.AdminMessages = service
	b.API.Base = server.URL
	in := incoming{owner: "bob", chat: 202}
	update := telegram.Update{ID: message.ID, Callback: &telegram.Callback{
		From: telegram.User{ID: in.chat}, Data: fmt.Sprintf("adminmsg:page:%d:20", message.ID),
		Message: telegram.Message{Chat: telegram.Chat{ID: in.chat, Type: "private"}},
	}}
	_, captureErr := b.saveBatch(t.Context(), 0, []telegram.Update{update})
	require.NoError(t, captureErr)
	// Inbox acknowledgement removes its payload; durable native proof remains.
	_, captureErr = db.Exec(t.Context(), `DELETE FROM bot.telegram_inbox WHERE update_id=$1`, update.ID)
	require.NoError(t, captureErr)
	ctx := withAdminMessageSource(t.Context(), in, update)
	messages := orderMessages{language: language}
	require.NoError(t, b.sendAdminMessagePage(ctx, in, message.ID, 20, &messages))
	readSaved := func() string {
		var saved string
		queryErr := db.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object('body',r.content,'effect',i.effect_key,'sequence',q.lane_sequence) ORDER BY q.lane_sequence),'[]'::jsonb)::text
 FROM bot.delivery_intents i JOIN core.delivery_queue q ON q.bot_id=i.bot_id AND q.owner_key=i.operation_key AND q.effect_key=i.effect_key AND q.owner_kind='bot'
 JOIN bot.interactions r ON r.owner=i.owner AND r.update_id=$1 AND r.kind=i.reference->>'result_kind'
 WHERE i.owner='bob' AND i.reference->>'update'=($1::bigint)::text`, message.ID).
			Scan(&saved)
		require.NoError(t, queryErr)
		return saved
	}
	original := readSaved()
	require.NotEqual(t, "[]", original)
	live.Store(second)
	// Reconstruct the caller as after a worker restart. The real host
	// boundary must retain bodies, complete effect set and lane order.
	restarted := b
	require.NoError(t, restarted.sendAdminMessagePage(ctx, in, message.ID, 20, &messages))
	require.JSONEq(t, original, readSaved())
	assertAdminMessageSavedNavigation(t, original, first, &messages, grow)
}

func assertAdminMessageSavedNavigation(
	t *testing.T,
	original string,
	first adminmessage.Page,
	messages *orderMessages,
	grow bool,
) {
	t.Helper()
	var results []struct {
		Body botdelivery.StoredResult `json:"body"`
	}
	require.NoError(t, json.Unmarshal([]byte(original), &results))
	navigation := 0
	var all strings.Builder
	for _, result := range results {
		all.WriteString(result.Body.Payload.Text)
		for _, row := range result.Body.Payload.Markup.Rows {
			for _, button := range row {
				if strings.HasPrefix(button.Data, "adminmsg:inspect:") {
					navigation++
				}
			}
		}
	}
	require.Equal(t, len(first.Items), navigation)
	require.Contains(t, all.String(), adminMessageProgressText(first.Progress, messages))
	if !grow {
		require.Greater(t, len(results), 1)
	}
}
