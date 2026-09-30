package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/assistantsource"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestAssistantSourceObservationDurableFreshnessAndDormantRegistry(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := knowledge.Service{DB: db}
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	require.NoError(t, runtime.RegisterAssistantSources(service))
	scrape := func() string {
		response := httptest.NewRecorder()
		runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, response.Code)
		return response.Body.String()
	}
	text := scrape()
	require.Contains(t, text, `zns_assistant_source_present{source="assistant_about"} 0`)
	identity := assistantsource.Digest([]byte("private-identity"))
	digest := assistantsource.Digest([]byte("private document"))
	require.NoError(t, service.ConfigureSource(t.Context(), knowledge.AssistantAbout, identity))
	require.NoError(t, service.ConfigureSource(t.Context(), knowledge.AssistantQA, ""))
	require.NoError(
		t,
		service.ReplaceSource(t.Context(), knowledge.AssistantAbout, identity, digest, []string{"private document"}),
	)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.assistant_sources SET refreshed_at=statement_timestamp()-interval '2 hours' WHERE slot='assistant_about'`,
	)
	require.NoError(t, err)
	require.NoError(t, service.SourceFailure(t.Context(), knowledge.AssistantAbout, identity, "source_timeout"))
	text = scrape()
	require.Contains(t, text, `zns_assistant_source_state{source="assistant_about",state="stale"} 1`)
	require.Contains(t, text, `zns_assistant_source_last_error{code="source_timeout",source="assistant_about"} 1`)
	require.Contains(t, text, `zns_assistant_source_version{source="assistant_about"} 1`)
	require.Contains(t, text, `zns_assistant_source_refresh_age_known{source="assistant_about"} 1`)
	require.Contains(t, text, `zns_assistant_source_refresh_age_known{source="assistant_qa"} 0`)
	require.Contains(t, text, `zns_assistant_source_last_error{code="none",source="assistant_qa"} 1`)
	for _, private := range []string{identity, digest, "private document", "private-identity"} {
		require.NotContains(t, text, private)
	}
	// Read-only scrapes must retain last-good publication and version.
	var retained string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT body FROM core.assistant_source_documents WHERE slot='assistant_about'`).
			Scan(&retained),
	)
	require.Equal(t, "private document", retained)
	require.NoError(
		t,
		service.ReplaceSource(t.Context(), knowledge.AssistantAbout, identity, digest, []string{"private document"}),
	)
	text = scrape()
	require.Contains(t, text, `zns_assistant_source_state{source="assistant_about",state="ready"} 1`)
	require.Contains(t, text, `zns_assistant_source_version{source="assistant_about"} 1`)
	_, err = db.Exec(t.Context(), `DELETE FROM core.assistant_sources WHERE slot='assistant_qa'`)
	require.NoError(t, err)
	text = scrape()
	require.Contains(t, text, `zns_assistant_source_present{source="assistant_qa"} 0`)
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, `source="assistant_qa"`) {
			require.Contains(t, line, "zns_assistant_source_present")
		}
	}
}

func TestAssistantSourceObservationBoundsPoolAcquisition(t *testing.T) {
	t.Parallel()
	db := database(t)
	options := db.Config()
	options.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), options)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	connection, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(connection.Release)
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	require.NoError(t, runtime.RegisterAssistantSources(knowledge.Service{DB: pool}))
	response := httptest.NewRecorder()
	started := time.Now()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Less(t, time.Since(started), 5*time.Second, "scrape acquisition uses the collector deadline")
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "zns_assistant_source_snapshot_available 0")
	require.NotContains(t, response.Body.String(), "zns_assistant_source_present{")
	connection.Release()
	response = httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "zns_assistant_source_snapshot_available 1")
	require.Contains(t, response.Body.String(), `zns_assistant_source_present{source="assistant_about"} 0`)
}
