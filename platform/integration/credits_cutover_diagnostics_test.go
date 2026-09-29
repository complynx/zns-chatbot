package integration_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

type cutoverIntakeTransport struct {
	activate func(context.Context) error
	polls    int
}

func (tr *cutoverIntakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil && strings.HasSuffix(request.URL.Path, "/getUpdates") {
		tr.polls++
		if tr.activate != nil {
			if activateErr := tr.activate(request.Context()); activateErr != nil {
				_ = response.Body.Close()
				return nil, activateErr
			}
			tr.activate = nil
		}
	}
	return response, err
}

func TestCreditsCutoverConfigurationStopsAndRecovers(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"enforcement", "override"} {
		for _, phase := range []string{"startup", "intake"} {
			t.Run(invalid+"/"+phase, func(t *testing.T) {
				t.Parallel()
				checkCutoverStopAndRecovery(t, invalid, phase)
			})
		}
	}
}

func checkCutoverStopAndRecovery(t *testing.T, invalid, phase string) {
	t.Helper()
	f := setup(t)
	f.b.CreditsEnforce = invalid != "enforcement"
	if invalid == "override" {
		f.b.AssistantDailyLimit = 5
	}
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	service := credits.Service{DB: f.db, Enforce: true}
	installCreditPrice(t, service)
	activate := func(ctx context.Context) error {
		return service.Operate(
			ctx,
			"bob",
			credits.OperatorChange{
				Kind:     "cutover",
				Key:      "diagnostic:epoch",
				Evidence: "synthetic drain and rollout evidence",
			},
		)
	}
	transport := &cutoverIntakeTransport{}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	if phase == "startup" {
		require.NoError(t, activate(t.Context()))
		_, err = f.db.Exec(t.Context(), `INSERT INTO bot.telegram_inbox(update_id,payload) VALUES($1,$2),($3,$4)`,
			1, message(1, 101, "private-cutover-canary"), 2, message(2, 101, "second pending"))
		require.NoError(t, err)
		_, err = f.db.Exec(
			t.Context(),
			`INSERT INTO bot.cursors(name,value) VALUES('telegram',0),('telegram_received',3)`,
		)
		require.NoError(t, err)
	} else {
		transport.activate = activate
		post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "private-cutover-canary"})
		post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "second pending"})
	}
	var output bytes.Buffer
	f.b.Logger = observability.NewLogger(&output, observability.LogConfig{})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	runErr := f.b.Run(ctx)
	require.Error(t, runErr, "configuration errors must terminate Run instead of retrying until cancellation")
	require.NoError(t, ctx.Err(), "Run must fail before the retry timeout")
	assert.Zero(t, f.model.calls)
	assert.NotContains(t, output.String(), "private-cutover-canary")
	assert.NotContains(t, output.String(), "bot retry pending")
	code, field := "credit_cutover_enforcement", "credits_enforce"
	if invalid == "override" {
		code, field = "credit_cutover_legacy_limit", "assistant_daily_limit"
	}
	assert.Contains(t, output.String(), `"code":"`+code+`"`)
	assert.Contains(t, output.String(), `"field":"`+field+`"`)
	assert.Contains(t, output.String(), `"reason":`)
	f.b.Logger.ErrorContext(t.Context(), "unknown failure", "error", errors.New("secret-provider-canary"))
	assert.Contains(t, output.String(), `"error":"operation failed"`)
	assert.NotContains(t, output.String(), "secret-provider-canary")
	if phase == "startup" {
		assert.Zero(t, transport.polls)
	} else {
		assert.Equal(t, 1, transport.polls)
	}
	assertCutoverPending(t, f)
	f.b.CreditsEnforce, f.b.AssistantDailyLimit = true, 0
	f.b.Model = meteredRemoteFixture{service}
	completeInbox(t, f, 3)
	var attempts, questions int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts WHERE enforced`).Scan(&attempts),
	)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.agent_quota`).Scan(&questions))
	assert.Equal(t, 2, attempts)
	assert.Zero(t, questions)
}

func assertCutoverPending(t *testing.T, f *fixture) {
	t.Helper()
	var pending, attempts, bindings int
	var processed int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts`).Scan(&attempts))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.budget_operations`).Scan(&bindings))
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram'`).Scan(&processed),
	)
	assert.Equal(t, 2, pending)
	assert.Zero(t, attempts)
	assert.Zero(t, bindings)
	assert.Zero(t, processed)
}
