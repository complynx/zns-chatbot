package telemetrylab_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/complynx/zns-chatbot/platform/internal/telemetrylab"
)

func TestCollectorCaptureBoundsAndFailure(t *testing.T) {
	t.Parallel()
	lab := (&telemetrylab.Lab{}).Handler()
	batch, err := proto.Marshal(
		&collector.ExportTraceServiceRequest{
			ResourceSpans: []*tracepb.ResourceSpans{
				{ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{Name: "telegram.update"}}}}},
			},
		},
	)
	require.NoError(t, err)
	for range 70 {
		recorder := httptest.NewRecorder()
		lab.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(batch)))
		require.Equal(t, http.StatusOK, recorder.Code)
	}
	read := httptest.NewRecorder()
	lab.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/lab/traces", nil))
	var snapshot struct {
		Batches  []json.RawMessage `json:"batches"`
		Received int               `json:"received"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &snapshot))
	assert.Equal(t, 70, snapshot.Received)
	assert.Len(t, snapshot.Batches, 64)
	assert.Contains(t, read.Body.String(), "telegram.update")
	failure := httptest.NewRequest(http.MethodPost, "/lab/failure", strings.NewReader(`{"enabled":true}`))
	failure.Header.Set("X-Sandbox", "1")
	set := httptest.NewRecorder()
	lab.ServeHTTP(set, failure)
	require.Equal(t, http.StatusNoContent, set.Code)
	rejected := httptest.NewRecorder()
	lab.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(batch)))
	assert.Equal(t, http.StatusServiceUnavailable, rejected.Code)
	large := httptest.NewRecorder()
	lab.ServeHTTP(
		large,
		httptest.NewRequest(
			http.MethodPost,
			"/v1/traces",
			strings.NewReader(strings.Repeat("x", telemetrylab.MaxWireBytes+1)),
		),
	)
	assert.Equal(t, http.StatusRequestEntityTooLarge, large.Code)
}
