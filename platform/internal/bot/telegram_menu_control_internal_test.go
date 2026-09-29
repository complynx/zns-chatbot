package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type menuControl struct{ attempts int }

func (p *menuControl) Admit(context.Context) (delivery.Admission, error) {
	p.attempts++
	if p.attempts%2 == 0 {
		return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Millisecond)}, nil
	}
	return delivery.Admission{Ready: true}, nil
}
func (*menuControl) Observe(_ context.Context, outcome delivery.Outcome) (delivery.Outcome, time.Time, error) {
	return outcome, time.Now(), nil
}

func TestRegisterTelegramMenuResumesExactDeferredStep(t *testing.T) {
	t.Parallel()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	policy := &menuControl{}
	b := Bot{TG: telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}}
	require.NoError(t, b.registerTelegramMenu(t.Context()))
	require.Equal(
		t,
		[]string{"/botsynthetic/setChatMenuButton", "/botsynthetic/setMyCommands", "/botsynthetic/setMyCommands"},
		methods,
	)
	require.Equal(t, 5, policy.attempts)
}
