package integration_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLostCreationSurvivesProxyAndAccessRejections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		typed  bool
	}{
		{http.StatusForbidden, false}, {http.StatusConflict, false}, {http.StatusForbidden, true},
	} {
		t.Run(strconv.Itoa(tc.status)+strconv.FormatBool(tc.typed), func(t *testing.T) {
			t.Parallel()
			testLostCreateReplay(t, tc.status, tc.typed)
		})
	}
}

func testLostCreateReplay(t *testing.T, status int, typed bool) {
	t.Helper()
	p := newPeers(t)
	p.loseCreate = true
	var bodies, keys []string
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs" && r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			p.mu.Lock()
			bodies = append(bodies, string(body))
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			reject := len(bodies) == 2
			p.mu.Unlock()
			if reject {
				if typed {
					w.WriteHeader(status)
					respond(w, map[string]any{"error": map[string]string{"code": "forbidden"}})
					return
				}
				http.Error(w, "proxy has no backend", status)
				return
			}
		}
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	id := h.submit(t, 501)
	if typed {
		require.Eventually(t, func() bool {
			rows, err := h.store.ListOutbox(t.Context(), id)
			return err == nil && len(rows) > 0 && strings.Contains(string(rows[0].Payload), "Agent creation")
		}, 10*time.Second, 20*time.Millisecond)
		awaitRecoveryDeliverySettled(t, h, id)
		h.stop(t)
		_, err := h.store.Pool().Exec(t.Context(),
			"UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
		require.NoError(t, err)
		h.start(t)
	}
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		current, err := h.store.GetTask(t.Context(), id)
		assert.NoError(collect, err)
		outbox, _ := h.store.ListOutbox(t.Context(), id)
		states := make([]string, len(outbox))
		for n := range outbox {
			states[n] = outbox[n].State
		}
		assert.Equal(collect, "completed", current.State, "outbox=%v", states)
	}, 15*time.Second, 25*time.Millisecond)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.GreaterOrEqual(t, len(bodies), 3)
	for n, body := range bodies {
		require.Equal(t, bodies[0], body)
		require.Equal(t, id.String(), keys[n])
	}
	require.Len(t, p.runs, 1)
	require.Len(t, p.createdInputs, 1)
}
