package appclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type archiveAdmissionTransport struct{ requests int }

func (tr *archiveAdmissionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tr.requests++
	return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Request: r,
		Body: io.NopCloser(strings.NewReader(`{"code":"invalid_json"}`))}, nil
}

func archiveAdmissionHost() appclient.Host {
	return appclient.Host{
		Base:      "http://archive.invalid",
		Signer:    identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))},
		UserToken: func(context.Context, string) (string, error) { return "current-token", nil },
	}
}

func callArchiveAdmission(ctx context.Context, host appclient.Host, input any) error {
	switch value := input.(type) {
	case conversation.OriginalArchive:
		return host.ArchiveOriginal(ctx, "alice", value.SourceKey, value.Kind, value.Text)
	case conversation.OutcomeArchive:
		return host.ArchiveOutcome(ctx, "alice", value.SourceKey, value.Text)
	case conversation.DerivedArchive:
		return host.ArchiveDerived(ctx, "alice", value.SourceKey, value.Text, value.ReplyToUpdateID,
			value.Media, *value.ExpectedGeneration, value.ReadAuthorities)
	default:
		panic("unexpected archive fixture")
	}
}

func oversizedArchiveInputs(huge string) map[string]any {
	generation := int64(0)
	inputs := map[string]any{
		"original-text": conversation.OriginalArchive{SourceKey: "key", Kind: "user", Text: huge},
		"original-key":  conversation.OriginalArchive{SourceKey: huge, Kind: "user", Text: "text"},
		"original-kind": conversation.OriginalArchive{SourceKey: "key", Kind: huge, Text: "text"},
		"outcome-text":  conversation.OutcomeArchive{SourceKey: "key", Text: huge},
		"outcome-key":   conversation.OutcomeArchive{SourceKey: huge, Text: "text"},
		"derived-text": conversation.DerivedArchive{SourceKey: "tg-assistant-1", Text: huge, ReplyToUpdateID: 1,
			ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{}},
		"derived-key": conversation.DerivedArchive{SourceKey: huge, Text: "text", ReplyToUpdateID: 1,
			ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{}},
	}
	for name, authorities := range map[string][]readsource.Authority{
		"count":        make([]readsource.Authority, readsource.MaxAuthorities+1),
		"causal-actor": {{Causal: &readsource.CausalSource{Actor: huge, Generation: &generation, Authorities: []readsource.Authority{}}}},
		"causal-count": {{Causal: &readsource.CausalSource{Actor: "alice", Generation: &generation, Authorities: make([]readsource.Authority, readsource.MaxAuthorities+1)}}},
		"registration": {{Registration: passbooking.ReadAuthority{Kind: passbooking.ReadOwnedEvent, Event: huge}}},
		"knowledge":    {{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: huge}}},
		"food":         {{Food: legacyfood.ReadAuthority{Event: huge, Scope: "review"}}},
		"practitioner": {{Practitioner: massage.ReadAuthority{Event: "event", Owner: huge}}},
	} {
		inputs[name] = conversation.DerivedArchive{SourceKey: "tg-assistant-1", Text: "text", ReplyToUpdateID: 1,
			ExpectedGeneration: &generation, ReadAuthorities: authorities}
	}
	return inputs
}

func TestHTTPArchiveAdmissionRejectsBeforeEncodingAndSend(t *testing.T) {
	t.Parallel()
	inputs := oversizedArchiveInputs(strings.Repeat("x", 2*1024*1024))
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			transport := &archiveAdmissionTransport{}
			host := archiveAdmissionHost()
			host.HTTP = &http.Client{Transport: transport}
			err := callArchiveAdmission(t.Context(), host, input)
			assert.Zero(t, transport.requests, "rejected archive must not reach HTTP transport")
			var problem *core.ProblemError
			require.ErrorAs(t, err, &problem)
			require.Equal(t, http.StatusBadRequest, problem.Status)
			require.Equal(t, "invalid_json", problem.Code)
		})
	}
}

func TestHTTPArchiveAdmissionPreservesAuthPrecedence(t *testing.T) {
	t.Parallel()
	transport := &archiveAdmissionTransport{}
	host := archiveAdmissionHost()
	host.HTTP = &http.Client{Transport: transport}
	var calls int
	host.UserToken = func(context.Context, string) (string, error) {
		calls++
		return "", identity.ErrZitadelIdentity
	}
	inputs := oversizedArchiveInputs(strings.Repeat("x", 70000))
	for _, input := range inputs {
		require.ErrorIs(t, callArchiveAdmission(t.Context(), host, input), identity.ErrZitadelIdentity)
	}
	require.Equal(t, len(inputs), calls)
	require.Zero(t, transport.requests)
}

func TestHTTPArchiveAdmissionExactLimit(t *testing.T) {
	t.Parallel()
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, conversation.MaxLiveArchiveBytes+1))
		assert.NoError(t, err)
		assert.Len(t, body, conversation.MaxLiveArchiveBytes)
		received.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err = io.WriteString(w, `{"ok":true}`)
		assert.NoError(t, err)
	}))
	defer server.Close()
	host := archiveAdmissionHost()
	host.Base, host.HTTP = server.URL, server.Client()
	generation := int64(0)
	for _, build := range []func(string) any{
		func(text string) any { return conversation.OriginalArchive{SourceKey: "key", Kind: "user", Text: text} },
		func(text string) any { return conversation.OutcomeArchive{SourceKey: "key", Text: text} },
		func(text string) any {
			return conversation.DerivedArchive{SourceKey: "tg-assistant-1", Text: text, ReplyToUpdateID: 1,
				ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{}}
		},
	} {
		prefix := "\"\\\n<>&\u2028🌍"
		encoded, err := json.Marshal(build(prefix))
		require.NoError(t, err)
		text := prefix + strings.Repeat("x", conversation.MaxLiveArchiveBytes-len(encoded))
		require.NoError(t, callArchiveAdmission(t.Context(), host, build(text)))
		var problem *core.ProblemError
		require.ErrorAs(t, callArchiveAdmission(t.Context(), host, build(text+"x")), &problem)
		require.Equal(t, "invalid_json", problem.Code)
	}
	require.EqualValues(t, 3, received.Load())
}

// Run with -run '^$' -bench '^BenchmarkHTTPArchiveAdmissionBoundedInputs$'
// so process-wide allocation measurements have no concurrent test workloads.
func BenchmarkHTTPArchiveAdmissionBoundedInputs(b *testing.B) {
	inputs := oversizedArchiveInputs(strings.Repeat("x", 2*1024*1024))
	for name, input := range inputs {
		b.Run(name, func(b *testing.B) {
			transport := &archiveAdmissionTransport{}
			host := archiveAdmissionHost()
			host.HTTP = &http.Client{Transport: transport}
			runtime.GC()
			b.ReportAllocs()
			for b.Loop() {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				err := callArchiveAdmission(b.Context(), host, input)
				runtime.ReadMemStats(&after)
				require.Error(b, err)
				require.Less(
					b,
					after.TotalAlloc-before.TotalAlloc,
					uint64(1024*1024),
					"oversized input must not be encoded",
				)
				require.Zero(b, transport.requests)
			}
		})
	}
}
