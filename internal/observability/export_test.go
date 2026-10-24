package observability_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestTraceExportUsesSignalPathAndPreservesExplicitPath(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []struct{ suffix, path string }{
		{"", "/v1/traces"}, {"/", "/v1/traces"}, {"/custom/traces", "/custom/traces"},
	} {
		t.Run(endpoint.suffix, func(t *testing.T) {
			t.Parallel()
			type export struct {
				method, path, contentType string
				body                      []byte
				err                       error
			}
			exports := make(chan export, 1)
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
				exports <- export{r.Method, r.URL.Path, r.Header.Get("Content-Type"), body, err}
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer collector.Close()
			telemetry, err := observability.New(context.Background(), observability.Options{
				OTLPEndpoint: collector.URL + endpoint.suffix, ServiceName: "zinusya-export-test", SampleRatio: 1,
			})
			require.NoError(t, err)
			_, finish := telemetry.Start(context.Background(), "queue")
			finish(nil)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			require.NoError(t, telemetry.Shutdown(ctx))
			select {
			case got := <-exports:
				require.NoError(t, got.err)
				require.Equal(t, http.MethodPost, got.method)
				require.Equal(t, endpoint.path, got.path)
				require.Equal(t, "application/x-protobuf", got.contentType)
				var request collectortrace.ExportTraceServiceRequest
				require.NoError(t, proto.Unmarshal(got.body, &request))
				resources := request.GetResourceSpans()
				require.Len(t, resources, 1)
				scopes := resources[0].GetScopeSpans()
				require.Len(t, scopes, 1)
				spans := scopes[0].GetSpans()
				require.Len(t, spans, 1)
				require.Equal(t, "queue", spans[0].GetName())
			default:
				t.Fatal("shutdown completed without an observed trace export")
			}
		})
	}
}
