package mediaproc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

type asrAccounting struct {
	fail        bool
	attempts    []credits.Attempt
	settlements []credits.Settlement
}

func (*asrAccounting) RequestTier() string { return "" }

func (r *asrAccounting) Reserve(_ context.Context, a credits.Attempt) error {
	r.attempts = append(r.attempts, a)
	if r.fail {
		return credits.ErrAccounting
	}
	return nil
}
func (*asrAccounting) Dispatch(context.Context, string) error { return nil }
func (r *asrAccounting) Settle(_ context.Context, _ string, s credits.Settlement) error {
	r.settlements = append(r.settlements, s)
	return nil
}

func TestASRCreditsCaptureBeforeTranscriptValidation(t *testing.T) {
	t.Parallel()
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write(
			[]byte(
				`{"text":null,"usage":{"type":"tokens","input_tokens":100,"output_tokens":4,"input_token_details":{"audio_tokens":90,"text_tokens":10}}}`,
			),
		)
	}))
	defer provider.Close()
	recorder := &asrAccounting{}
	worker := Worker{
		config: Config{
			APIKey:     "synthetic",
			APIURL:     provider.URL,
			Model:      "synthetic-transcribe",
			HTTPClient: provider.Client(),
			Accounting: recorder,
		},
	}
	ctx := credits.WithScope(t.Context(), credits.Scope{Actor: "user", Payer: "user", Key: "voice:1"})
	transcript := worker.recognize(ctx, []byte("synthetic-wav"))
	require.Equal(t, statusFailed, transcript.Status)
	require.Equal(t, 1, calls)
	require.Len(t, recorder.settlements, 1)
	require.Equal(t, int64(90), *recorder.settlements[0].Usage.AudioInput)
	require.Equal(t, "unknown", recorder.settlements[0].CostBasis)
	require.Equal(t, "user", recorder.attempts[0].Scope.Payer)
	recorder.fail = true
	transcript = worker.recognize(ctx, []byte("synthetic-wav"))
	require.Equal(t, statusFailed, transcript.Status)
	require.Equal(t, 1, calls, "accounting failure must prevent provider dispatch")
}

func TestASRRequestHeaderSurvivesInvalidResponse(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-ID", "req_synthetic_audio")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private invalid response"))
			}))
			defer provider.Close()
			recorder := &asrAccounting{}
			worker := Worker{
				config: Config{
					APIKey:     "synthetic",
					APIURL:     provider.URL,
					Model:      "test",
					HTTPClient: provider.Client(),
					Accounting: recorder,
				},
			}
			result := worker.recognize(t.Context(), []byte("synthetic-wav"))
			require.Equal(t, statusFailed, result.Status)
			require.Len(t, recorder.settlements, 1)
			require.Equal(t, "req_synthetic_audio", recorder.settlements[0].Usage.RequestID)
			require.Equal(t, "unknown", recorder.settlements[0].Usage.Basis)
		})
	}
}
