package api

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestDatabaseErrorResponsesPreservePublicContract(t *testing.T) {
	t.Parallel()
	for _, responder := range []struct {
		name    string
		respond func(*slog.Logger, http.ResponseWriter, any, error)
	}{{"general", respond}, {"knowledge", respondKnowledge}, {"profile", respondPassProfile}} {
		t.Run(responder.name, func(t *testing.T) {
			t.Parallel()
			for _, sample := range []struct {
				name   string
				err    error
				marked bool
				status int
				code   string
			}{
				{"sql", &pgconn.PgError{Code: "P0001", Message: "private SQL password=secret"}, true, 500, "internal_error"},
				{"marked", core.DatabaseFailure(&core.ProblemError{Status: 503, Code: "unavailable"}), true, 503, "unavailable"},
				{"provider", errors.New("provider unavailable"), false, 500, "internal_error"},
				{"domain", &core.ProblemError{Status: 403, Code: "forbidden"}, false, 403, "forbidden"},
			} {
				t.Run(sample.name, func(t *testing.T) {
					t.Parallel()
					var logs bytes.Buffer
					recorder := httptest.NewRecorder()
					responder.respond(slog.New(slog.NewTextHandler(&logs, nil)), recorder, nil, sample.err)
					require.Equal(t, sample.status, recorder.Code)
					assert.JSONEq(t, `{"code":"`+sample.code+`"}`, recorder.Body.String())
					assert.Equal(t, sample.marked, recorder.Header().Get(core.DatabaseFailureHeader) == "1")
					assert.NotContains(t, recorder.Body.String()+logs.String(), "private SQL")
					assert.NotContains(t, recorder.Body.String()+logs.String(), "password=secret")
				})
			}
		})
	}
}
