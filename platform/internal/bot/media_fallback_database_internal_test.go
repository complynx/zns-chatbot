package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type r36FailureTransport struct {
	status int
	code   string
	marked bool
	calls  int
}

func (f *r36FailureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	header := make(http.Header)
	if f.marked {
		header.Set(core.DatabaseFailureHeader, "1")
	}
	body, _ := json.Marshal(map[string]string{"code": f.code})
	return &http.Response{
		StatusCode: f.status, Header: header, Body: io.NopCloser(strings.NewReader(string(body))), Request: r,
	}, nil
}

func r36FailureBot(f *r36FailureTransport) *Bot {
	api := appclient.Client{
		Base: "http://core.test", HTTP: &http.Client{Transport: f}, SandboxToken: (identity.Signer{}).Token,
	}
	return &Bot{API: api, Host: appclient.Host{Base: api.Base, HTTP: api.HTTP, UserToken: api.UserToken}}
}

func TestMediaFallbackPreservesDatabaseCause(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden}
	for _, failure := range []error{
		core.DatabaseFailure(denial), core.ErrDatabase, errors.Join(core.ErrDatabase, context.Canceled),
	} {
		b := &Bot{}
		require.ErrorIs(t, b.mediaExecutionError(t.Context(), incoming{}, mediaIntake{}, failure), core.ErrDatabase)
		require.False(t, terminalMediaUploadFailure(failure))
	}
	for _, failure := range []error{denial, telegram.ErrInvalidDocument, &telegram.APIError{Code: http.StatusBadRequest}} {
		require.True(t, terminalMediaUploadFailure(failure))
	}
	require.False(t, terminalMediaUploadFailure(io.EOF))
	require.ErrorIs(t, (&Bot{}).mediaExecutionError(t.Context(), incoming{}, mediaIntake{}, io.EOF), io.EOF)
}

func TestMediaHTTPFallbackPreservesMarkedAbsence(t *testing.T) {
	t.Parallel()
	for _, marked := range []bool{false, true} {
		transport := &r36FailureTransport{status: http.StatusNotFound, code: "media_not_found", marked: marked}
		b := r36FailureBot(transport)
		notice, err := b.acceptAVRange(
			t.Context(), "alice", "attachment", "", agent.MediaProposal{}, &agent.Input{}, mediaproc.Result{},
		)
		if marked {
			require.ErrorIs(t, err, core.ErrDatabase)
			require.Empty(t, notice)
		} else {
			require.NoError(t, err)
			require.Equal(t, i18n.MediaUnavailable, notice)
		}
		require.Equal(t, 1, transport.calls)
		transport.calls = 0
		input := agent.Input{MediaContext: &agent.MediaContext{}, OrderHistory: []orders.Change{
			{OrderID: "order", Action: stateProof, State: stateProof, Version: 2},
		}}
		err = b.addKnownReceipt(t.Context(), "alice", &input)
		if marked {
			require.ErrorIs(t, err, core.ErrDatabase)
			require.Nil(t, input.MediaContext.LatestKnownReceipt)
		} else {
			require.NoError(t, err)
			require.Equal(t, "order", input.MediaContext.LatestKnownReceipt.OrderID)
		}
		require.Equal(t, 1, transport.calls)
	}
}

func TestMediaExecutionSQLDoesNotTerminalize(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	_, err := db.Exec(t.Context(), `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,status,model_text)
 VALUES('r36','alice',36,'attachment','choose','pending context')`)
	require.NoError(t, err)
	transport := &r36FailureTransport{status: http.StatusServiceUnavailable, code: "provider_unavailable"}
	b := r36FailureBot(transport)
	b.DB = db
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden}
	err = b.mediaExecutionError(
		t.Context(), incoming{owner: "alice"}, mediaIntake{ID: "r36"}, core.DatabaseFailure(denial),
	)
	require.ErrorIs(t, err, core.ErrDatabase)
	var status, modelText string
	err = db.QueryRow(t.Context(), `SELECT status,model_text FROM bot.media_intake WHERE id='r36'`).
		Scan(&status, &modelText)
	require.NoError(t, err)
	require.Equal(t, mediaChoose, status)
	require.Equal(t, "pending context", modelText)
	require.Zero(t, transport.calls)
	// The ordinary denial retains terminalization even if rendering then cannot reach the provider.
	err = b.mediaExecutionError(t.Context(), incoming{owner: "alice"}, mediaIntake{ID: "r36"}, denial)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	err = db.QueryRow(t.Context(), `SELECT status,model_text FROM bot.media_intake WHERE id='r36'`).
		Scan(&status, &modelText)
	require.NoError(t, err)
	require.Equal(t, mediaDone, status)
	require.Empty(t, modelText)
	require.Equal(t, 1, transport.calls)
}
