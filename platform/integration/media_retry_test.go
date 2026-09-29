package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type mediaRetryTransport struct {
	path     string
	status   int
	response string
	calls    atomic.Int64
}

func (transport *mediaRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasPrefix(request.URL.Path, transport.path) && transport.calls.Add(1) == 1 {
		return &http.Response{
			StatusCode: transport.status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(transport.response)),
			Request:    request,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestMediaUploadTransientRetry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, path, response string
		status               int
		core                 bool
	}{
		{name: "telegram download503", path: "/file/", status: http.StatusServiceUnavailable, response: "temporarily unavailable"},
		{name: "telegram getFile429", path: "/botsandbox/getFile", status: http.StatusTooManyRequests, response: `{"ok":false,"error_code":429,"description":"retry later"}`},
		{name: "core upload503", path: "/v1/media", status: http.StatusServiceUnavailable, response: `{"code":"temporarily_unavailable"}`, core: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			photo, original := intakePhoto(t, f)
			f.model.plan = agent.Plan{
				View:        agent.MediaView,
				MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "avatar"},
			}
			transport := &mediaRetryTransport{path: test.path, status: test.status, response: test.response}
			client := &http.Client{Transport: transport}
			if test.core {
				f.b.API.HTTP = client
				f.b.Host.HTTP = f.b.API.HTTP
			} else {
				f.b.TG.HTTP = client
			}
			require.Error(
				t,
				f.b.Handle(t.Context(), photo),
				"transient errors must reach the poller before it advances the cursor",
			)
			assert.Zero(t, f.model.calls)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.media_intake WHERE id='tg-media-100'`).
					Scan(&count),
			)
			assert.Zero(t, count)
			handle(t, f.b, photo)
			require.Equal(t, 1, f.model.calls)
			require.NotNil(t, f.model.input.Attachment)
			assert.Equal(t, original, f.model.input.Attachment.Body)
			var stored []byte
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT media.body FROM core.media media JOIN bot.media_intake intake ON intake.attachment_id=media.id WHERE intake.id='tg-media-100'`).
					Scan(&stored),
			)
			assert.True(t, bytes.Equal(original, stored), "the successful retry stores immutable original bytes")
			before := transport.calls.Load()
			handle(t, f.b, photo)
			assert.Equal(t, before, transport.calls.Load(), "completed upload retry does not download/upload again")
			assert.Equal(t, 1, f.model.calls, "completed update reuses interpretation")
		})
	}
}

func TestMediaInvalidUploadDoesNotRetry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	update := message(100, 101, "")
	update.Message.Document = &telegram.Document{
		FileID:   "too-large",
		Filename: "oversized.bin",
		Size:     telegram.MaxDocumentBytes + 1,
	}
	require.NoError(t, f.b.Handle(t.Context(), update), "definitive input rejection returns a terminal user notice")
	assert.Zero(t, f.model.calls)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&count),
	)
	assert.Zero(t, count)
}

func TestMediaPriceSnapshotFailureCannotResolveAmbiguity(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := intakeOrder(t, f, "first")
	second := intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
	f.b.API.HTTP = &http.Client{Transport: &mediaRetryTransport{
		path:   "/v1/order-events/" + first.EventID + "/orders/" + first.ID + "/payment-instructions",
		status: http.StatusServiceUnavailable, response: `{"code":"temporarily_unavailable"}`,
	}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), photo), "incomplete price snapshots cannot establish a unique match")
	assert.Zero(t, f.model.calls)
	handle(t, f.b, photo)
	assert.Equal(t, 1, f.model.calls)
	require.NotNil(t, f.model.input.MediaContext)
	assert.Len(t, f.model.input.MediaContext.Candidates, 2)
	for _, id := range []string{first.ID, second.ID} {
		current, err := f.b.API.Order(t.Context(), "alice", first.EventID, id)
		require.NoError(t, err)
		assert.Equal(t, "unpaid", current.State)
	}
}

func TestMediaExpiredIncompleteUploadTerminatesRetry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order := intakeOrder(t, f, "first")
	photo, _ := intakePhoto(t, f)
	f.b.API.HTTP = &http.Client{Transport: &mediaRetryTransport{
		path:   "/v1/order-events/" + order.EventID + "/orders/" + order.ID + "/payment-instructions",
		status: http.StatusServiceUnavailable, response: `{"code":"temporarily_unavailable"}`,
	}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), photo))
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE bot.media_intake SET expires_at=now()-interval '1 second' WHERE id='tg-media-100'`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.Handle(t.Context(), photo), "expired upload must not pin the global update cursor")
	assert.Zero(t, f.model.calls)
	var status, notice string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT status,notice FROM bot.media_intake WHERE id='tg-media-100'`).
			Scan(&status, &notice),
	)
	assert.Equal(t, "done", status)
	assert.Equal(t, "media.unavailable", notice)
	require.NoError(t, f.b.Handle(t.Context(), photo), "repeated expired delivery stays terminal")
	f.model.plan = agent.Plan{View: "workflow", Text: "That attachment expired."}
	handle(t, f.b, message(101, 101, "Was that receipt submitted?"))
	require.NotEmpty(t, f.model.input.MediaContext.Recent)
	assert.Equal(t, "media.unavailable", f.model.input.MediaContext.Recent[0].Outcome)
}

func TestMediaMissingSourceTerminatesIncompleteRetry(t *testing.T) {
	t.Parallel()
	for _, retired := range []bool{false, true} {
		t.Run(fmt.Sprintf("retired=%t", retired), func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			order := intakeOrder(t, f, "first")
			photo, _ := intakePhoto(t, f)
			f.b.API.HTTP = &http.Client{Transport: &mediaRetryTransport{
				path:   "/v1/order-events/" + order.EventID + "/orders/" + order.ID + "/payment-instructions",
				status: http.StatusServiceUnavailable, response: `{"code":"temporarily_unavailable"}`,
			}}
			f.b.Host.HTTP = f.b.API.HTTP
			require.Error(t, f.b.Handle(t.Context(), photo))
			_, err := f.db.Exec(
				t.Context(),
				`DELETE FROM core.media WHERE id=(SELECT attachment_id FROM bot.media_intake WHERE id='tg-media-100')`,
			)
			require.NoError(t, err)
			if retired {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE bot.media_intake SET status='done',notice='media.unavailable' WHERE id='tg-media-100'`,
				)
				require.NoError(t, err)
			}
			require.NoError(t, f.b.Handle(t.Context(), photo), "missing source is a terminal input outcome")
			require.NoError(t, f.b.Handle(t.Context(), photo), "retired inputs do not reload unavailable bytes")
			assert.Zero(t, f.model.calls)
			var status string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT status FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&status),
			)
			assert.Equal(t, "done", status)
		})
	}
}
