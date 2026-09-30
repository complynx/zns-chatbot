package bot

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type r34Transport func(*http.Request) (*http.Response, error)

func (f r34Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func r34Response(r *http.Request, status int, body string, database bool) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	if database {
		header.Set(core.DatabaseFailureHeader, "1")
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

type r34Batch struct {
	name, path string
	call       func(*Bot, context.Context) error
}

func r34Batches() []r34Batch {
	return []r34Batch{
		{"orders", "/internal/notifications", (*Bot).DeliverNotifications},
		{"passes", "/internal/pass-notifications", (*Bot).DeliverPassNotifications},
		{"food", "/internal/food/notifications", (*Bot).DeliverFoodNotifications},
		{"massage", "/internal/massage-notifications", (*Bot).DeliverMassageNotifications},
		{"recover_orders", "/internal/notifications/recover", (*Bot).RecoverOrderNotifications},
		{"recover_passes", "/internal/pass-notifications/recover", (*Bot).RecoverPassNotifications},
		{"recover_food", "/internal/food/notifications/recover", (*Bot).RecoverFoodNotifications},
		{"recover_massage", "/internal/massage-notifications/recover", (*Bot).RecoverMassageNotifications},
	}
}

func r34Pending(path string) string {
	if path == "/internal/massage-notifications" {
		return `[{"owner":"alice"},{"owner":"bob"}]`
	}
	result := `[{"id":1},{"id":2},{"id":3}]`
	if strings.Contains(path, "massage") {
		result = `[{"owner":"alice","notice":{"id":1}},{"owner":"alice","notice":{"id":2}},{"owner":"alice","notice":{"id":3}}]`
	}
	if strings.HasSuffix(path, "/recover") {
		result = strings.ReplaceAll(result, `"id":`, `"followup_pending":true,"id":`)
	}
	return result
}

func TestNotificationBatchStopsAfterDatabaseFailure(t *testing.T) {
	t.Parallel()
	for _, batch := range r34Batches() {
		for _, mode := range []string{"database", "ordinary", "database_cancelled"} {
			t.Run(batch.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				fixture := &r34BatchFixture{batch: batch, mode: mode, cancel: cancel}
				b := &Bot{Host: appclient.Host{Base: "http://core.test", HTTP: &http.Client{Transport: fixture}}}
				fixture.assertResult(t, batch.call(b, ctx))
			})
		}
	}
}

type r34BatchFixture struct {
	batch       r34Batch
	mode        string
	cancel      context.CancelFunc
	completions int
	requests    []string
}

func (f *r34BatchFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, r.URL.Path)
	switch {
	case r.URL.Path == "/internal/pass-announcements/claim":
		return r34Response(r, 200, `{"found":false}`, false), nil
	case r.URL.Path == f.batch.path:
		return r34Response(r, 200, r34Pending(f.batch.path), false), nil
	case strings.HasSuffix(r.URL.Path, "/pending"):
		return r34Response(r, 200, r34Pending("massage"), false), nil
	default:
		return f.complete(r)
	}
}

func (f *r34BatchFixture) complete(r *http.Request) (*http.Response, error) {
	f.completions++
	if f.completions == 1 {
		return r34Response(r, 403, `{"code":"first_recipient"}`, false), nil
	}
	if f.completions == 2 {
		if f.mode == "ordinary" {
			return nil, io.EOF
		}
		if f.mode == "database_cancelled" {
			f.cancel()
		}
		return r34Response(r, 500, `{"code":"database_unavailable"}`, true), nil
	}
	return r34Response(r, 200, `{"ok":true}`, false), nil
}

func (f *r34BatchFixture) assertResult(t *testing.T, err error) {
	t.Helper()
	require.ErrorContains(t, err, "first_recipient")
	if f.mode == "ordinary" {
		require.False(t, core.IsDatabaseFailure(err))
		require.ErrorContains(t, err, "core API unavailable")
		expected := 3
		if f.batch.name == "massage" {
			expected = 6
		}
		require.Equal(t, expected, f.completions)
		return
	}
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Equal(t, 2, f.completions, "later notices must retain their pending state")
	require.NotContains(t, f.requests, "/internal/massage-notifications/bob/pending")
	if f.mode == "database_cancelled" {
		require.ErrorIs(t, err, context.Canceled)
	}
}
func TestNotificationBatchStopsAtAnnouncementOrRecipientLookup(t *testing.T) {
	t.Parallel()
	for _, batch := range []r34Batch{
		{"announcement", "/internal/pass-announcements/claim", (*Bot).DeliverPassNotifications},
		{"recipient", "/internal/massage-notifications/alice/pending", (*Bot).DeliverMassageNotifications},
	} {
		t.Run(batch.name, func(t *testing.T) {
			t.Parallel()
			var requests []string
			transport := r34Transport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.Path)
				if r.URL.Path == batch.path {
					return r34Response(r, 500, `{"code":"database_unavailable"}`, true), nil
				}
				return r34Response(r, 200, r34Pending(r.URL.Path), false), nil
			})
			b := &Bot{Host: appclient.Host{Base: "http://core.test", HTTP: &http.Client{Transport: transport}}}
			require.ErrorIs(t, batch.call(b, t.Context()), core.ErrDatabase)
			require.Equal(t, batch.path, requests[len(requests)-1])
			require.NotContains(t, requests, "/internal/pass-notifications")
			require.NotContains(t, requests, "/internal/massage-notifications/bob/pending")
		})
	}
}

func TestNotificationBatchCancellationRemainsControl(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	transport := r34Transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return r34Response(r, 200, r34Pending("orders"), false), nil
		}
		cancel()
		return nil, context.Canceled
	})
	b := &Bot{Host: appclient.Host{Base: "http://core.test", HTTP: &http.Client{Transport: transport}}}
	err := b.DeliverNotifications(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
	require.Equal(t, 2, calls)
}

func TestNotificationBatchRetainsMassageLimit(t *testing.T) {
	t.Parallel()
	completions := 0
	var requests []string
	notices := `[` + strings.TrimSuffix(strings.Repeat(`{"notice":{"id":1}},`, 12), ",") + `]`
	transport := r34Transport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path == "/internal/massage-notifications" {
			return r34Response(r, 200, r34Pending(r.URL.Path), false), nil
		}
		if strings.HasSuffix(r.URL.Path, "/pending") {
			return r34Response(r, 200, notices, false), nil
		}
		completions++
		return r34Response(r, 200, `{"ok":true}`, false), nil
	})
	b := &Bot{Host: appclient.Host{Base: "http://core.test", HTTP: &http.Client{Transport: transport}}}
	require.NoError(t, b.DeliverMassageNotifications(t.Context()))
	require.Equal(t, massageDeliveryBatch, completions)
	require.NotContains(t, requests, "/internal/massage-notifications/bob/pending")
}
