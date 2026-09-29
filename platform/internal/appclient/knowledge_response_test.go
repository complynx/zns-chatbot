package appclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func knowledgeResponseClient(t *testing.T, payload []byte) appclient.Client {
	t.Helper()
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { _, err := w.Write(payload); assert.NoError(t, err) },
		),
	)
	t.Cleanup(server.Close)
	return appclient.Client{Base: server.URL, SandboxToken: func(string) string { return "test-user" }}
}

func TestKnowledgeAuthorityBudgetDoesNotConsumeBodyBudget(t *testing.T) {
	t.Parallel()
	generation := int64(0)
	refs := []readsource.Authority{}
	for range 80 {
		refs = append(
			refs,
			readsource.Authority{
				Causal: &readsource.CausalSource{
					Actor:       strings.Repeat("a", 200),
					Generation:  &generation,
					Authorities: []readsource.Authority{},
				},
			},
		)
	}
	require.True(t, readsource.Valid(refs))
	expected := knowledge.Memo{
		Key:             "key",
		Text:            strings.Repeat("x", appclient.MaxAPIBytes-128),
		Version:         1,
		Active:          true,
		ReadAuthorities: refs,
	}
	data, err := json.Marshal(expected)
	require.NoError(t, err)
	require.Greater(t, len(data), appclient.MaxAPIBytes)
	client := knowledgeResponseClient(t, data)
	actual, err := client.Memo(t.Context(), "alice", "key")
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	var generic knowledge.Memo
	require.Error(
		t,
		client.Call(t.Context(), "alice", http.MethodGet, "/ordinary", nil, &generic),
		"unrelated response allowance is unchanged",
	)
}

func TestKnowledgeResponseRejectsInvalidAuthorityAndOversizedBody(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"null":          `{"key":"key","text":"body","read_authorities":null}`,
		"invalid_leaf":  `{"key":"key","text":"body","read_authorities":[{}]}`,
		"unknown_field": `{"key":"key","text":"body","read_authorities":[{"causal":{"actor":"alice","generation":0,"authorities":[],"forged":true}}]}`,
		"oversized_body": `{"key":"key","text":"` + strings.Repeat(
			"x",
			appclient.MaxAPIBytes,
		) + `","read_authorities":[]}`,
		"oversized_metadata": `{"key":"key","text":"body","read_authorities":["` + strings.Repeat(
			"x",
			readsource.MaxAuthorityBytes,
		) + `"]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := knowledgeResponseClient(t, []byte(payload))
			memo, err := client.Memo(t.Context(), "alice", "key")
			require.Error(t, err)
			require.Empty(t, memo.Text)
		})
	}
}

func TestKnowledgeCollectionRestoresSingleAuthorityUnion(t *testing.T) {
	t.Parallel()
	generation := int64(0)
	refs := []readsource.Authority{
		{
			Causal: &readsource.CausalSource{
				Actor:       "alice",
				Generation:  &generation,
				Authorities: []readsource.Authority{},
			},
		},
	}
	data, err := json.Marshal(
		knowledge.MemoPage{
			Items:           []knowledge.Memo{{Key: "one", Text: "body"}, {Key: "two", Text: "other"}},
			ReadAuthorities: refs,
		},
	)
	require.NoError(t, err)
	client := knowledgeResponseClient(t, data)
	values, err := client.Memos(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, refs, values[0].ReadAuthorities)
	require.Equal(t, refs, values[1].ReadAuthorities)
	*values[0].ReadAuthorities[0].Causal.Generation = 7
	require.Zero(t, *values[1].ReadAuthorities[0].Causal.Generation, "each host result owns its evidence copy")
}
