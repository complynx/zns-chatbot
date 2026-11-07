package integration_test

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const settingsProxyMaxFrame = 16 << 20

// commitReplyProxy forwards real PostgreSQL traffic. Once armed, it closes the
// connection after receiving COMMIT completion but before the client receives it.
type commitReplyProxy struct {
	dsn     string
	armed   atomic.Bool
	dropped chan struct{}
}

func newCommitReplyProxy(t *testing.T, dsn string) *commitReplyProxy {
	t.Helper()
	backend, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "disable", backend.Query().Get("sslmode"), "proxy requires the isolated cleartext test connection")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	proxied := *backend
	proxied.Host = listener.Addr().String()
	p := &commitReplyProxy{dsn: proxied.String(), dropped: make(chan struct{})}
	var active sync.Map
	var handlers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			front, e := listener.Accept()
			if e != nil {
				return
			}
			active.Store(front, true)
			handlers.Go(func() {
				defer active.Delete(front)
				p.forward(front, backend.Host)
			})
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		active.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		handlers.Wait()
	})
	return p
}

func (p *commitReplyProxy) forward(front net.Conn, address string) {
	defer func() { _ = front.Close() }()
	back, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return
	}
	defer func() { _ = back.Close() }()
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(back, front)
		_ = back.Close()
		close(copied)
	}()
	defer func() { _ = front.Close(); _ = back.Close(); <-copied }()
	for {
		header := make([]byte, 5)
		if _, err = io.ReadFull(back, header); err != nil {
			return
		}
		length := binary.BigEndian.Uint32(header[1:])
		if length < 4 || length > settingsProxyMaxFrame {
			return
		}
		body := make([]byte, length-4)
		if _, err = io.ReadFull(back, body); err != nil {
			return
		}
		if header[0] == 'C' && string(body) == "COMMIT\x00" && p.armed.CompareAndSwap(true, false) {
			close(p.dropped)
			return
		}
		if _, err = front.Write(header); err != nil {
			return
		}
		if _, err = front.Write(body); err != nil {
			return
		}
	}
}

func TestAgentSettingsLostDatabaseCommitReplyRecoversOriginalReceipt(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	args.Document.AuthCacheTTLSeconds = 120
	body := prepareSettingsBody(t, h, operator, "lost-settings-commit", args)
	proxy := newCommitReplyProxy(t, h.store.Pool().Config().ConnString())
	store, err := db.Open(t.Context(), proxy.dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	h.store = store
	h.start(t)
	h.stop(t)
	proxy.armed.Store(true)
	code, raw := adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	select {
	case <-proxy.dropped:
	case <-time.After(time.Second):
		t.Fatal("the actual COMMIT reply was not dropped")
	}
	receipt, err := h.store.SettingsChangeReceipt(t.Context(), scope, "lost-settings-commit")
	require.NoError(t, err)
	require.Equal(t, args.Document, receipt.Document)
	require.Equal(t, args.ExpectedVersion+1, receipt.Version)
	require.EqualValues(t, 1, settingsAuditCount(t, h))
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "lost-settings-commit"})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, *receipt, readSettingsResult(t, raw))
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, *receipt, readSettingsResult(t, raw))
	require.EqualValues(t, 1, settingsAuditCount(t, h))
}
