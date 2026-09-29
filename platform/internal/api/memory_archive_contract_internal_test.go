package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestArchiveRoutesRejectPublicAndCrossClassInputs(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	mux := http.NewServeMux()
	memoryArchiveRoutes(mux, conversation.Service{}, signer, slog.Default())
	for _, route := range []string{"original", "derived", "outcome"} {
		for _, token := range []string{"", signer.Token("alice"), signer.DeliveryToken()} {
			req := httptest.NewRequest(http.MethodPost, "/internal/history/archive/"+route, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			require.Equal(t, http.StatusUnauthorized, response.Code)
		}
	}
	cases := []struct{ route, body string }{
		{"original", `{"source_key":"tg-assistant-1","kind":"assistant","text":"model"}`},
		{"original", `{"source_key":"x","kind":"user","text":"model","runtime_derived":false}`},
		{"original", `{"source_key":"x","kind":"user","text":"model","owner":"bob"}`},
		{"outcome", `{"source_key":"x","text":"model","kind":"assistant"}`},
		{"outcome", `{"source_key":"x","text":"model","expected_generation":0}`},
		{"derived", `{"source_key":"tg-assistant-1","text":"model","reply_to_update_id":1,"read_authorities":[]}`},
		{"derived", `{"source_key":"tg-assistant-1","text":"model","reply_to_update_id":1,"expected_generation":0}`},
		{
			"derived",
			`{"source_key":"tg-assistant-1","text":"model","reply_to_update_id":1,"expected_generation":0,"read_authorities":null}`,
		},
		{
			"derived",
			`{"source_key":"tg-assistant-2","text":"model","reply_to_update_id":1,"expected_generation":0,"read_authorities":[]}`,
		},
	}
	for _, test := range cases {
		req := httptest.NewRequest(
			http.MethodPost,
			"/internal/history/archive/"+test.route,
			strings.NewReader(test.body),
		)
		req.Header.Set("Authorization", "Bearer "+signer.MemoryProvenanceToken("alice"))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		require.Equal(t, http.StatusBadRequest, response.Code, test.body)
	}
}

func TestDerivedArchiveZeroGenerationIsExplicit(t *testing.T) {
	t.Parallel()
	var input conversation.DerivedArchive
	require.NoError(
		t,
		json.Unmarshal(
			[]byte(
				`{"source_key":"tg-assistant-1","text":"model","reply_to_update_id":1,"expected_generation":0,"read_authorities":[]}`,
			),
			&input,
		),
	)
	require.NotNil(t, input.ExpectedGeneration)
	require.Zero(t, *input.ExpectedGeneration)
	input.ExpectedGeneration = nil
	require.NoError(t, json.Unmarshal([]byte(`{"source_key":"tg-assistant-1"}`), &input))
	require.Nil(t, input.ExpectedGeneration)
}
