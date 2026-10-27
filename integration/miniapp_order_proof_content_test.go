package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func orderProofContentFixture(t *testing.T) (paymentFixture, appSession, uuid.UUID) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.fileBytes = []byte("%PDF-1.4\nprivate native order file")
	f, _, _ := selectionFixtureWithPeers(t, p)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, f.H.service.Stop(ctx))
	f.H.service = nil
	file := paymentFile(t, f.H, f.OwnerID, 61701, "native-order-preview", time.Now().UTC().Truncate(time.Second))
	return f, detailsAppSession(t, f.H, 11002, 116), file
}

func TestMiniAppOrderProofContentChecksOwnerAndUsesSafeAttachmentHeaders(t *testing.T) {
	t.Parallel()
	f, session, file := orderProofContentFixture(t)
	path := ordersAppPath(f.Event.ID) + "/proof-files/" + file.String() + "/content"
	code, raw, headers := appCall(t, f.H, http.MethodGet, path, "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "private native order file")
	require.Equal(t, "attachment", headers.Get("Content-Disposition"))
	require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Contains(t, headers.Get("Content-Security-Policy"), "sandbox")
	foreign := paymentFile(
		t,
		f.H,
		f.AmbassadorID,
		61702,
		"foreign-native-preview",
		time.Now().UTC().Truncate(time.Second),
	)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		ordersAppPath(f.Event.ID)+"/proof-files/"+foreign.String()+"/content",
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	require.NotContains(t, string(raw), "private native order file")
}

func TestMiniAppOrderProofContentRechecksAccessAndLocatorAfterSourceIO(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"session-logout", "module-removed", "locator-changed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, session, file := orderProofContentFixture(t)
			done := make(chan error, 1)
			f.H.peers.mu.Lock()
			f.H.peers.fileReadHook = func() {
				var err error
				switch kind {
				case "session-logout":
					var code int
					code, _, _, err = appHTTPCall(
						f.H,
						http.MethodPost,
						"/app/session/logout",
						`{}`,
						session.Token,
						appTestOrigin,
					)
					if err == nil && code != http.StatusNoContent {
						err = fmt.Errorf("logout status %d", code)
					}
				case "module-removed":
					_, err = f.H.store.Pool().
						Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY[]::text[] WHERE id=$1", f.Event.ID)
				case "locator-changed":
					_, err = f.H.store.Pool().
						Exec(t.Context(), "UPDATE shuttle_files SET telegram_id='different-source' WHERE id=$1", file)
				}
				done <- err
			}
			f.H.peers.mu.Unlock()
			code, raw, _ := appCall(
				t,
				f.H,
				http.MethodGet,
				ordersAppPath(f.Event.ID)+"/proof-files/"+file.String()+"/content",
				"",
				session.Token,
				"",
			)
			expected := http.StatusNotFound
			if kind == "session-logout" {
				expected = http.StatusUnauthorized
			}
			if kind == "locator-changed" {
				expected = http.StatusConflict
			}
			require.Equal(t, expected, code, string(raw))
			require.NotContains(t, string(raw), "private native order file")
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("source read hook did not execute")
			}
		})
	}
}
