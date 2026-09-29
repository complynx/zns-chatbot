package scriptclient

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestHostOperationDeadlineStopsRun(t *testing.T) {
	t.Parallel()
	host, stop := context.WithCancelCause(context.WithValue(t.Context(), callbackIdentityKey{}, "trusted"))
	defer stop(nil)
	var calls int
	d := callbackDispatch{ctx: host, stopRun: stop, allowed: map[string]bool{"orders.get": true},
		callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
			calls++
			require.Equal(t, "trusted", ctx.Value(callbackIdentityKey{}))
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	_, err := d.callBounded(t.Context(), callbackRequest(t), 30*time.Millisecond)
	require.Error(t, err)
	require.ErrorIs(t, context.Cause(host), context.DeadlineExceeded)
	_, err = d.call(t.Context(), callbackRequest(t))
	require.Error(t, err)
	require.Equal(t, 1, calls, "a caught callback failure cannot start another operation")
}

func TestCallbacksShareEarlierWholeRunDeadline(t *testing.T) {
	t.Parallel()
	whole, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	host, stop := context.WithCancelCause(whole)
	defer stop(nil)
	calls := 0
	d := callbackDispatch{ctx: host, stopRun: stop, allowed: map[string]bool{"orders.get": true},
		callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
			calls++
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			want, _ := whole.Deadline()
			require.Equal(t, want, deadline, "a callback must not renew the whole run")
			if calls == 1 {
				timer := time.NewTimer(200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
					return json.RawMessage("1"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	_, err := d.callBounded(t.Context(), callbackRequest(t), time.Second)
	require.NoError(t, err)
	_, err = d.callBounded(t.Context(), callbackRequest(t), time.Second)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	require.ErrorIs(t, context.Cause(host), context.DeadlineExceeded)
}

func TestComposedRPCAllowsBoundedHostWorkBeyondFiveSeconds(t *testing.T) {
	t.Parallel()
	host, cancel := context.WithTimeout(t.Context(), scriptprotocol.ExecuteTransportTimeout)
	defer cancel()
	host, stop := context.WithCancelCause(host)
	defer stop(nil)
	application, worker := net.Pipe()
	defer application.Close()
	defer worker.Close()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); _ = scriptworker.ServeRPC(t.Context(), worker, worker) }()
	var calls atomic.Int32
	d := callbackDispatch{ctx: host, stopRun: stop, allowed: map[string]bool{"orders.get": true},
		callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
			calls.Add(1)
			timer := time.NewTimer(2700 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				return json.RawMessage("1"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}
	client := jrpc2.NewClient(
		channel.Line(application, application),
		&jrpc2.ClientOptions{OnCallback: handler.New(d.call)},
	)
	defer client.Close()
	started := time.Now()
	var response scriptworker.Response
	err := client.CallResult(host, "execute", scriptprotocol.ExecuteRequest{
		Code:  "const a=await tools.orders.get({});const b=await tools.orders.get({});return a+b;",
		Input: json.RawMessage("null"), Tools: []scriptprotocol.Tool{{Name: "orders.get"}},
	}, &response)
	require.NoError(t, err)
	require.Empty(t, response.Error)
	require.JSONEq(t, "2", string(response.Result))
	require.EqualValues(t, 2, calls.Load())
	require.Greater(t, time.Since(started), 5*time.Second)
	require.NoError(t, client.Close())
	<-workerDone
	t.Logf(
		"callbacks=%d elapsed=%s active_limit=%s host_limit=%s whole_limit=%s",
		calls.Load(),
		time.Since(started),
		scriptprotocol.ActiveTime,
		scriptprotocol.HostCallTimeout,
		scriptprotocol.ExecuteTimeout,
	)
}

func TestExpiredCallbackDoesNotPublishLateReceiptOrRepeatEffect(t *testing.T) {
	t.Parallel()
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_commit", true: "after_commit"}[committed], func(t *testing.T) {
			t.Parallel()
			host, stop := context.WithCancelCause(t.Context())
			defer stop(nil)
			effects := 0
			calls := 0
			d := callbackDispatch{ctx: host, stopRun: stop, allowed: map[string]bool{"orders.get": true},
				callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
					calls++
					if committed {
						effects++
					}
					<-ctx.Done()
					return json.RawMessage("{\"committed\":true}"), nil
				}}
			result, err := d.callBounded(t.Context(), callbackRequest(t), 30*time.Millisecond)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Nil(t, result, "late payload must not become a successful VM result")
			_, err = d.call(t.Context(), callbackRequest(t))
			require.Error(t, err)
			require.Equal(t, 1, calls)
			if committed {
				require.Equal(t, 1, effects)
			} else {
				require.Zero(t, effects)
			}
		})
	}
}
