package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

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
				// Replaying the ingress uses the same immutable effect keys and bodies.
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
		case "/internal/bot-delivery/result":
			var request adminPageTestResult
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				errors <- err
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// The real storage boundary validates this composed outbound payload.
			if _, err := telegram.PrepareSend(request.Result.Payload); err != nil {
				errors <- err
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requests <- request
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
