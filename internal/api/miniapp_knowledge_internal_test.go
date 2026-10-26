package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/miniapp"
)

func TestMiniAppKnowledgeSessionErrorsDistinguishEditorAccessFromAuthentication(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		status int
	}{
		{db.ErrKnowledgeDenied, http.StatusForbidden},
		{miniapp.ErrInvalid, http.StatusUnauthorized},
		{db.ErrMiniAppDenied, http.StatusUnauthorized},
		{db.ErrConflict, http.StatusConflict},
	} {
		response := httptest.NewRecorder()
		miniAppKnowledgeSessionError(response, tc.err)
		require.Equal(t, tc.status, response.Code)
	}
}

func TestMiniAppKnowledgeInputRejectsAmbiguousOrOversizedRequests(t *testing.T) {
	t.Parallel()
	type args struct {
		Reason string `json:"reason"`
	}
	cases := []struct{ name, body string }{
		{"null", "null"},
		{"array", "[]"},
		{"unknown field", `{"reason":"edit","subject":"someone else"}`},
		{"trailing object", `{"reason":"edit"}{}`},
		{"oversized", `{"reason":"` + strings.Repeat("a", knowledgeTransportLimit) + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/app/knowledge/edit/prepare", strings.NewReader(tc.body))
			_, err := decodeMiniAppKnowledge[args](httptest.NewRecorder(), r)
			require.Error(t, err)
		})
	}
	nested := httptest.NewRequest(
		http.MethodPost,
		"/app/knowledge/edit/prepare",
		strings.NewReader(`{"operation_id":"known","arguments":{"reason":"edit","actor":"someone else"}}`),
	)
	_, nestedErr := decodeMiniAppKnowledge[miniAppKnowledgePrepareInput[args]](httptest.NewRecorder(), nested)
	require.Error(t, nestedErr)
	r := httptest.NewRequest(
		http.MethodPost,
		"/app/knowledge/edit/prepare",
		strings.NewReader(`{"reason":"correction"}`),
	)
	result, err := decodeMiniAppKnowledge[args](httptest.NewRecorder(), r)
	require.NoError(t, err)
	require.Equal(t, "correction", result.Reason)
}

func TestMiniAppKnowledgeModeratorRequiresExplicitEnabledAndVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, arguments string
		valid           bool
	}{
		{"missing arguments", "", false},
		{"null arguments", "null", false},
		{"missing enabled", `{"expected_moderator_version":0}`, false},
		{"null enabled", `{"moderator_enabled":null,"expected_moderator_version":0}`, false},
		{"missing version", `{"moderator_enabled":false}`, false},
		{"null version", `{"moderator_enabled":false,"expected_moderator_version":null}`, false},
		{"explicit false and zero", `{"moderator_enabled":false,"expected_moderator_version":0}`, true},
		{"explicit true and version", `{"moderator_enabled":true,"expected_moderator_version":7}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fields := map[string]json.RawMessage{
				"operation_id": json.RawMessage(`"954c5e5f-0ac1-4c89-96fc-c3448a03ea36"`),
			}
			if tc.arguments != "" {
				fields["arguments"] = json.RawMessage(tc.arguments)
			}
			body, err := json.Marshal(fields)
			require.NoError(t, err)
			for _, confirm := range []bool{false, true} {
				request := httptest.NewRequest(
					http.MethodPost,
					"/app/knowledge/set_moderator/prepare",
					strings.NewReader(string(body)),
				)
				var args miniAppKnowledgeModeratorArguments
				if confirm {
					decoded, decodeErr := decodeMiniAppKnowledge[miniAppKnowledgeConfirmInput[miniAppKnowledgeModeratorArguments]](
						httptest.NewRecorder(),
						request,
					)
					require.NoError(t, decodeErr)
					args = decoded.Arguments
				} else {
					decoded, decodeErr := decodeMiniAppKnowledge[miniAppKnowledgePrepareInput[miniAppKnowledgeModeratorArguments]](
						httptest.NewRecorder(),
						request,
					)
					require.NoError(t, decodeErr)
					args = decoded.Arguments
				}
				change, validateErr := miniAppKnowledgeWriteChange(args, miniAppKnowledgeModeratorChange)
				if !tc.valid {
					require.Error(t, validateErr)
					response := httptest.NewRecorder()
					miniAppKnowledgeError(response, validateErr)
					require.Equal(t, http.StatusBadRequest, response.Code)
					continue
				}
				require.NoError(t, validateErr)
				require.NotNil(t, change.ModeratorEnabled)
				require.Equal(t, *args.ModeratorEnabled, *change.ModeratorEnabled)
				require.Equal(t, *args.ExpectedModeratorVersion, change.ExpectedModeratorVersion)
			}
		})
	}
}
