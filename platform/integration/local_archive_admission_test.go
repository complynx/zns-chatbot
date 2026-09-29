package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestLiveArchiveAdmissionRawHTTPNewline(t *testing.T) {
	t.Parallel()
	service := conversation.Service{DB: database(t)}
	_, host := hostHistoryBoundary(t, service, "http")
	var sequence int64
	for _, route := range []string{"original", "outcome", "derived"} {
		for _, extra := range []int{0, 1} {
			sequence++
			input := archiveAdmissionInput{route: route, key: fmt.Sprintf("tg-assistant-%d", sequence), reply: sequence}
			encoded, err := json.Marshal(input.wire())
			require.NoError(t, err)
			input.text = strings.Repeat("x", liveArchiveWireLimit-1-len(encoded)+extra)
			encoded, err = json.Marshal(input.wire())
			require.NoError(t, err)
			encoded = append(encoded, '\n')
			require.Len(t, encoded, liveArchiveWireLimit+extra)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
				host.Base+"/internal/history/archive/"+route, bytes.NewReader(encoded))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+host.Signer.MemoryProvenanceToken("alice"))
			response, err := host.HTTP.Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			want := http.StatusOK
			if extra != 0 {
				want = http.StatusBadRequest
			}
			require.Equal(t, want, response.StatusCode, route)
			var count int
			require.NoError(t, service.DB.QueryRow(
				t.Context(),
				`SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND source_key=$1`,
				input.key,
			).Scan(&count))
			require.Equal(t, 1-extra, count)
		}
	}
}

const liveArchiveWireLimit = 65536

type archiveAdmissionInput struct {
	route string
	key   string
	text  string
	reply int64
	media bool
}

func (a archiveAdmissionInput) wire() any {
	switch a.route {
	case "original":
		return map[string]any{"source_key": a.key, "kind": "user", "text": a.text}
	case "outcome":
		return map[string]any{"source_key": a.key, "text": a.text}
	default:
		generation := int64(0)
		return conversation.DerivedArchive{SourceKey: a.key, Text: a.text, ReplyToUpdateID: a.reply,
			Media: a.media, ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{}}
	}
}

func (a archiveAdmissionInput) archive(ctx context.Context, host appclient.Host) error {
	switch a.route {
	case "original":
		return host.ArchiveOriginal(ctx, "alice", a.key, "user", a.text)
	case "outcome":
		return host.ArchiveOutcome(ctx, "alice", a.key, a.text)
	default:
		return host.ArchiveDerived(ctx, "alice", a.key, a.text, a.reply, a.media, 0, []readsource.Authority{})
	}
}

func TestLiveArchiveAdmissionBoundary(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"http", "local"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			service := conversation.Service{DB: database(t)}
			_, host := hostHistoryBoundary(t, service, transport)
			var sequence int64
			for _, route := range []string{"original", "outcome", "derived"} {
				t.Run(route, func(t *testing.T) {
					for _, prefix := range []string{"", strings.Repeat("\"\\\n<>&\u2028🌍", 100)} {
						for _, extra := range []int{0, 1} {
							sequence++
							input := archiveAdmissionInput{route: route, key: fmt.Sprintf("tg-assistant-%d", sequence),
								text: prefix, reply: sequence, media: true}
							requireArchiveAdmission(t, service, host, input, extra)
						}
					}
				})
			}
		})
	}
}

func TestLiveArchiveAdmissionPreservesImporter(t *testing.T) {
	t.Parallel()
	service := conversation.Service{DB: database(t)}
	text := strings.Repeat("x", 70000)
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "imported", "assistant", text))
	var count int
	require.NoError(t, service.DB.QueryRow(t.Context(),
		`SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND source_key='imported'`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestLiveArchiveAdmissionAuthenticatesBeforeSizeCheck(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"local", "http"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			service := conversation.Service{DB: database(t)}
			_, host := hostHistoryBoundary(t, service, transport)
			var calls int
			host.UserToken = func(context.Context, string) (string, error) {
				calls++
				return "", identity.ErrZitadelIdentity
			}
			for _, route := range []string{"original", "outcome", "derived"} {
				input := archiveAdmissionInput{route: route, key: "tg-assistant-1", reply: 1,
					text: strings.Repeat("x", liveArchiveWireLimit+1)}
				require.ErrorIs(t, input.archive(t.Context(), host), identity.ErrZitadelIdentity)
			}
			require.Equal(t, 3, calls)
			var count int
			require.NoError(
				t,
				service.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events`).Scan(&count),
			)
			require.Zero(t, count)
		})
	}
}

func requireArchiveAdmission(
	t *testing.T,
	service conversation.Service,
	host appclient.Host,
	input archiveAdmissionInput,
	extra int,
) {
	t.Helper()
	encoded, err := json.Marshal(input.wire())
	require.NoError(t, err)
	input.text += strings.Repeat("x", liveArchiveWireLimit-len(encoded)+extra)
	encoded, err = json.Marshal(input.wire())
	require.NoError(t, err)
	require.Len(t, encoded, liveArchiveWireLimit+extra)
	err = input.archive(t.Context(), host)
	if extra == 0 {
		require.NoError(t, err, "%s exact limit", input.route)
	} else {
		requireCode(t, err, "invalid_json")
	}
	var count int
	require.NoError(t, service.DB.QueryRow(
		t.Context(),
		`SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND source_key=$1`,
		input.key,
	).Scan(&count))
	require.Equal(t, 1-extra, count, "%s rejected requests must not persist", input.route)
}
