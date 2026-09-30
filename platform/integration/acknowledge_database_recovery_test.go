package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type acknowledgementFailureControl struct{ observed atomic.Int32 }

func (*acknowledgementFailureControl) Admit(context.Context) (delivery.Admission, error) {
	return delivery.Admission{Ready: true}, nil
}

func (c *acknowledgementFailureControl) Observe(
	context.Context, delivery.Outcome,
) (delivery.Outcome, time.Time, error) {
	c.observed.Add(1)
	return delivery.Outcome{}, time.Time{}, core.DatabaseOperationError(io.EOF)
}

type acknowledgementTransport struct{ calls atomic.Int32 }

func (tr *acknowledgementTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(request.URL.Path, "/answerCallbackQuery") {
		return http.DefaultTransport.RoundTrip(request)
	}
	tr.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`)),
		Request:    request,
	}, nil
}

func TestAcknowledgementSQLFailurePreservesInboxAndLanguageReceipt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	// Start at the persisted inbox boundary: the same callback is replayed by Run.
	payload, err := json.Marshal(aliceCallback(1, 0, "language:ru"))
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO bot.telegram_inbox(update_id,payload) VALUES(1,$1)`, string(payload))
	require.NoError(t, err)
	control := &acknowledgementFailureControl{}
	transport := &acknowledgementTransport{}
	f.b.TG.Control = control
	f.b.TG.HTTP = &http.Client{Transport: transport}
	runInboxDatabaseFailure(t, f)
	require.EqualValues(t, 1, control.observed.Load())
	require.EqualValues(t, 1, transport.calls.Load(), "acknowledgement is not retried inside the failed runtime")
	var pending, receipts int
	var language string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM bot.telegram_inbox WHERE update_id=1),
 (SELECT count(*) FROM core.language_operations WHERE owner='alice'),
 (SELECT language FROM core.users WHERE id='alice')`).Scan(&pending, &receipts, &language))
	require.Equal(t, 1, pending)
	require.Equal(t, 1, receipts)
	require.Equal(t, "ru", language)
	// Ordinary Telegram 429 remains best-effort after the SQL fault is removed.
	f.b.TG.Control = nil
	completeInbox(t, f, 2)
	require.EqualValues(t, 2, transport.calls.Load())
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).Scan(&receipts),
	)
	require.Equal(t, 1, receipts, "replaying the retained callback must not create another language operation")
}
