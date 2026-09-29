package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

type rejectedAccounting struct{}

func (rejectedAccounting) RequestTier() string { return "" }

func (rejectedAccounting) Reserve(context.Context, credits.Attempt) error {
	return credits.ErrAccounting
}
func (rejectedAccounting) Dispatch(context.Context, string) error { return credits.ErrAccounting }
func (rejectedAccounting) Settle(context.Context, string, credits.Settlement) error {
	return credits.ErrAccounting
}

func TestOpenAICreditsFailurePreventsSend(t *testing.T) {
	t.Parallel()
	calls := 0
	provider := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusBadGateway) },
		),
	)
	defer provider.Close()
	model := OpenAI{Key: "synthetic", BaseURL: provider.URL, HTTP: provider.Client(), Accounting: rejectedAccounting{}}
	_, err := model.structured(
		t.Context(),
		providerPrompt{name: "zns_history_summary", schema: `{}`, input: []byte(`{}`)},
	)
	require.ErrorIs(t, err, credits.ErrAccounting)
	require.Zero(t, calls)
}
