package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type stalledDelaySink struct {
	write   bool
	entered chan struct{}
	resume  chan struct{}
	skip    int
	calls   int
}

func (s *stalledDelaySink) Write(body []byte) (int, error) {
	if s.write {
		if s.calls == s.skip {
			close(s.entered)
			<-s.resume
		}
		s.calls++
	}
	return len(body), nil
}
func (s *stalledDelaySink) Sync() error {
	if !s.write {
		if s.calls == s.skip {
			close(s.entered)
			<-s.resume
		}
		s.calls++
	}
	return nil
}
func (*stalledDelaySink) Close() error { return nil }

func TestEditDelayStalledEvidenceInvalidatesWithoutReleaseAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{true, false} {
		t.Run(map[bool]string{true: "write", false: "sync"}[write], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			sink := &stalledDelaySink{write: write, entered: make(chan struct{}), resume: make(chan struct{})}
			journal := newDelayJournal(ctx, sink)
			journal.deadline = 50 * time.Millisecond
			t.Cleanup(func() {
				cancel()
				close(sink.resume)
				select {
				case <-journal.ended:
				case <-time.After(time.Second):
					t.Error("finite worker did not finish")
				}
			})
			d := &editDelay{
				ctx:         ctx,
				journal:     journal,
				state:       "held_before_apply",
				release:     make(chan struct{}),
				invalidated: make(chan struct{}),
				key:         strings.Repeat("k", 24),
			}
			wait := make(chan bool, 1)
			go func() { wait <- d.waitRelease() }()
			r := httptest.NewRequest(http.MethodPost, "/control/release", nil)
			r.Header.Set("X-R104-Control", d.key)
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); d.control(response, r) }()
			select {
			case <-sink.entered:
			case <-time.After(time.Second):
				t.Fatal("writer did not start")
			}
			state := httptest.NewRecorder()
			get := httptest.NewRequest(http.MethodGet, "/control/state", nil)
			get.Header.Set("X-R104-Control", d.key)
			d.control(state, get)
			require.Equal(t, http.StatusOK, state.Code)
			require.True(
				t,
				strings.Contains(state.Body.String(), "releasing") ||
					strings.Contains(state.Body.String(), "invalidated"),
			)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stalled evidence blocked control")
			}
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Equal(t, "invalidated", delayTestState(d))
			select {
			case released := <-wait:
				require.False(t, released)
			case <-time.After(time.Second):
				t.Fatal("invalidation failed to end selected wait")
			}
			select {
			case <-d.release:
				t.Fatal("release woke without durable evidence")
			default:
			}
			require.Error(t, d.record(delayEvent{Kind: "forward_started"}))
			require.Empty(t, d.events)
		})
	}
}

func TestEditDelayStalledReleaseNeverAppliesRealAdmittedEdit(t *testing.T) {
	t.Parallel()
	for _, write := range []bool{true, false} {
		t.Run(map[bool]string{true: "write", false: "sync"}[write], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			sink := &stalledDelaySink{write: write, skip: 1, entered: make(chan struct{}), resume: make(chan struct{})}
			journal := newDelayJournal(ctx, sink)
			journal.deadline = 50 * time.Millisecond
			t.Cleanup(func() {
				cancel()
				close(sink.resume)
				select {
				case <-journal.ended:
				case <-time.After(time.Second):
					t.Error("finite writer did not finish")
				}
			})
			digest := sha256.Sum256([]byte("replacement"))
			d := &editDelay{
				ctx:         ctx,
				journal:     journal,
				state:       "armed",
				release:     make(chan struct{}),
				invalidated: make(chan struct{}),
				dataSlots:   make(chan struct{}, 4),
				key:         strings.Repeat("k", 24),
				arm: delayArm{
					ChatID:     101,
					MessageID:  7,
					Mode:       "before_apply",
					TextSHA256: hex.EncodeToString(digest[:]),
				},
			}
			f := &Fake{
				Token:    "TOKEN",
				delay:    d,
				messages: []telegram.Message{{ID: 7, Chat: telegram.Chat{ID: 101, Type: "private"}, Text: "old"}},
			}
			done := make(chan any, 1)
			go func() {
				defer func() { done <- recover() }()
				delayTestRequest(
					f,
					http.MethodPost,
					"/botTOKEN/editMessageText",
					`{"chat_id":101,"message_id":7,"text":"replacement"}`,
				)
			}()
			delayTestWait(t, d, "held_before_apply")
			request := httptest.NewRequest(http.MethodPost, "/control/release", nil)
			request.Header.Set("X-R104-Control", d.key)
			response := httptest.NewRecorder()
			d.control(response, request)
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			select {
			case panicValue := <-done:
				require.Equal(t, http.ErrAbortHandler, panicValue)
			case <-time.After(time.Second):
				t.Fatal("stalled evidence retained real edit")
			}
			require.Equal(t, "invalidated", delayTestState(d))
			require.Equal(t, 0, f.edits)
			require.Equal(t, "old", f.messages[0].Text)
			require.Len(t, d.events, 1)
			require.Equal(t, "received", d.events[0].Kind)
			select {
			case <-d.release:
				t.Fatal("real held edit was released without durable evidence")
			default:
			}
		})
	}
}

func TestEditDelayBoundsDataBeforeHeadersWithReservedControl(t *testing.T) {
	t.Parallel()
	f, d := delayTestFake(t, "replacement", "before_apply")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	bounded := f.DelayListener(ctx, listener)
	server := &http.Server{Handler: f.Handler(), ReadHeaderTimeout: time.Second}
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(bounded) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, server.Close())
		select {
		case serveErr := <-ended:
			require.ErrorIs(t, serveErr, http.ErrServerClosed)
		case <-time.After(time.Second):
			t.Error("data server did not stop")
		}
	})
	connections := make([]net.Conn, 0, 4)
	for range 4 {
		connection, dialErr := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		require.NoError(t, dialErr)
		connections = append(connections, connection)
		t.Cleanup(func() { _ = connection.Close() })
		_, err = io.WriteString(connection, "GET /healthz HTTP/1.1\r\n")
		require.NoError(t, err)
	}
	require.Eventually(
		t,
		func() bool { return len(bounded.(*delayListener).slots) == 4 },
		time.Second,
		time.Millisecond,
	)
	fifth, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	require.NoError(t, err)
	defer fifth.Close()
	_, err = io.WriteString(fifth, "GET /healthz HTTP/1.1\r\nHost: local\r\nConnection: close\r\n\r\n")
	require.NoError(t, err)
	require.NoError(t, fifth.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, err = fifth.Read(make([]byte, 1))
	var networkErr net.Error
	require.ErrorAs(t, err, &networkErr)
	require.True(t, networkErr.Timeout())
	control := httptest.NewUnstartedServer(http.HandlerFunc(d.control))
	control.Listener = &delayListener{Listener: control.Listener, ctx: ctx, slots: make(chan struct{}, 2)}
	control.Start()
	defer control.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, control.URL+"/control/state", nil)
	require.NoError(t, err)
	request.Header.Set("X-R104-Control", d.key)
	response, err := control.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.NoError(t, connections[0].Close())
	require.NoError(t, fifth.SetReadDeadline(time.Now().Add(time.Second)))
	data, err := io.ReadAll(fifth)
	require.NoError(t, err)
	require.Contains(t, string(data), "200 OK")
	plain := &Fake{}
	require.Same(t, listener, plain.DelayListener(ctx, listener))
}
