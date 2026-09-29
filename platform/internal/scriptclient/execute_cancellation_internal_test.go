package scriptclient

import (
	"context"
	"encoding/json"
	"errors"
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

type callbackIdentityKey struct{}

func callbackRequest(t *testing.T) *jrpc2.Request {
	t.Helper()
	requests, err := jrpc2.ParseRequests(
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tool.call","params":{"name":"orders.get","arguments":{}}}`),
	)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Nil(t, requests[0].Error)
	return requests[0].ToRequest()
}

func TestCallbackCancellationKeepsTrustedContext(t *testing.T) {
	t.Parallel()
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "rpc", true: "host"}[parent], func(t *testing.T) {
			t.Parallel()
			host, cancelHost := context.WithCancel(context.WithValue(t.Context(), callbackIdentityKey{}, "trusted"))
			defer cancelHost()
			rpc, cancelRPC := context.WithCancel(context.WithValue(t.Context(), callbackIdentityKey{}, "untrusted"))
			defer cancelRPC()
			started := make(chan any, 1)
			done := make(chan error, 1)
			var count atomic.Int32
			dispatch := callbackDispatch{
				ctx:     host,
				allowed: map[string]bool{"orders.get": true},
				callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
					count.Add(1)
					started <- ctx.Value(callbackIdentityKey{})
					<-ctx.Done()
					return nil, ctx.Err()
				},
			}
			request := callbackRequest(t)
			go func() { _, err := dispatch.call(rpc, request); done <- err }()
			require.Equal(t, "trusted", <-started)
			if parent {
				cancelHost()
			} else {
				cancelRPC()
			}
			select {
			case err := <-done:
				require.Error(t, err)
			case <-time.After(time.Second):
				cancelHost()
				<-done
				t.Fatal("callback did not follow cancellation")
			}
			_, err := dispatch.call(rpc, request)
			require.Error(t, err)
			require.EqualValues(t, 1, count.Load(), "no callback may start after cancellation")
			if !parent {
				require.NoError(t, host.Err())
			}
		})
	}
}

func TestRPCDisconnectCancelsAndJoinsHostCallback(t *testing.T) {
	t.Parallel()
	host, cancel := context.WithCancel(t.Context())
	defer cancel()
	application, worker := net.Pipe()
	defer application.Close()
	defer worker.Close()
	started := make(chan struct{})
	finished := make(chan struct{})
	dispatch := callbackDispatch{
		ctx:     host,
		allowed: map[string]bool{"orders.get": true},
		callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(finished)
			return nil, ctx.Err()
		},
	}
	client := jrpc2.NewClient(
		channel.Line(application, application),
		&jrpc2.ClientOptions{OnCallback: handler.New(dispatch.call)},
	)
	defer client.Close()
	_, err := worker.Write(
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tool.call","params":{"name":"orders.get","arguments":{}}}` + "\n"),
	)
	require.NoError(t, err)
	<-started
	require.NoError(t, worker.Close())
	closed := make(chan struct{})
	go func() { _ = client.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		cancel()
		<-closed
		t.Fatal("RPC close did not cancel and join host callback")
	}
	<-finished
	require.NoError(t, host.Err())
}

func TestCallbackLifetimeEndsOnReturnAndQueuedCancellation(t *testing.T) {
	t.Parallel()
	captured := make(chan (<-chan struct{}), 1)
	count := 0
	dispatch := callbackDispatch{
		ctx:     t.Context(),
		allowed: map[string]bool{"orders.get": true},
		callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
			captured <- ctx.Done()
			count++
			return json.RawMessage(`42`), nil
		},
	}
	request := callbackRequest(t)
	_, err := dispatch.call(t.Context(), request)
	require.NoError(t, err)
	select {
	case <-<-captured:
	default:
		t.Fatal("callback context remained alive after return")
	}
	require.NoError(t, t.Context().Err())
	rpc, cancel := context.WithCancel(t.Context())
	dispatch.lock.Lock()
	done := make(chan error, 1)
	go func() { _, callErr := dispatch.call(rpc, request); done <- callErr }()
	cancel()
	dispatch.lock.Unlock()
	require.Error(t, <-done)
	require.Equal(t, 1, count)
}

func TestWorkerTimeoutCancelsHostCallbackOnClientClose(t *testing.T) {
	t.Parallel()
	host, cancel := context.WithCancel(t.Context())
	defer cancel()
	application, worker := net.Pipe()
	defer application.Close()
	defer worker.Close()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); serveShortDeadlineWorker(t.Context(), worker) }()
	finished := make(chan struct{})
	dispatch := callbackDispatch{
		ctx:     host,
		allowed: map[string]bool{"orders.get": true},
		callback: func(ctx context.Context, _ ToolCall) (json.RawMessage, error) {
			<-ctx.Done()
			close(finished)
			return nil, ctx.Err()
		},
	}
	client := jrpc2.NewClient(
		channel.Line(application, application),
		&jrpc2.ClientOptions{OnCallback: handler.New(dispatch.call)},
	)
	defer client.Close()
	requestCtx, stop := context.WithTimeout(t.Context(), scriptprotocol.ExecuteTransportTimeout)
	defer stop()
	var response scriptworker.Response
	err := client.CallResult(requestCtx, "execute", scriptprotocol.ExecuteRequest{
		Code:  `return await tools.orders.get({});`,
		Input: json.RawMessage(`null`),
		Tools: []scriptprotocol.Tool{{Name: "orders.get"}},
	}, &response)
	require.NoError(t, err)
	require.Equal(t, "timeout", response.Error)
	closed := make(chan struct{})
	go func() { _ = client.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		cancel()
		<-closed
		t.Fatal("worker timeout response did not release host callback")
	}
	<-finished
	<-workerDone
	require.NoError(t, host.Err())
}

func TestRPCShutdownJoinsFiniteContextIgnoringDependency(t *testing.T) {
	t.Parallel()
	application, worker := net.Pipe()
	defer application.Close()
	defer worker.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	dispatch := callbackDispatch{
		ctx:     t.Context(),
		allowed: map[string]bool{"orders.get": true},
		callback: func(context.Context, ToolCall) (json.RawMessage, error) {
			close(started)
			<-release
			return json.RawMessage(`42`), nil
		},
	}
	client := jrpc2.NewClient(
		channel.Line(application, application),
		&jrpc2.ClientOptions{OnCallback: handler.New(dispatch.call)},
	)
	defer client.Close()
	_, err := worker.Write(
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tool.call","params":{"name":"orders.get","arguments":{}}}` + "\n"),
	)
	require.NoError(t, err)
	<-started
	require.NoError(t, worker.Close())
	closed := make(chan struct{})
	go func() { _ = client.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("close returned while host dependency was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close failed to join completed dependency")
	}
}

// serveShortDeadlineWorker keeps transport alive while a real engine deadline
// expires. The worker package separately verifies its production RPC response.
func serveShortDeadlineWorker(ctx context.Context, stream net.Conn) {
	methods := handler.Map{
		"execute": handler.New(
			func(callCtx context.Context, request scriptprotocol.ExecuteRequest) (scriptworker.Response, error) {
				callCtx, cancel := context.WithTimeout(callCtx, 100*time.Millisecond)
				defer cancel()
				_, err := scriptworker.Execute(
					callCtx,
					request,
					func(callbackCtx context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
						response, callErr := jrpc2.ServerFromContext(callbackCtx).
							Callback(callbackCtx, "tool.call", call)
						if callErr != nil {
							return nil, callErr
						}
						var value json.RawMessage
						callErr = response.UnmarshalResult(&value)
						return value, callErr
					},
				)
				if !errors.Is(err, context.DeadlineExceeded) {
					return scriptworker.Response{}, err
				}
				return scriptworker.Response{Error: "timeout"}, nil
			},
		),
	}
	server := jrpc2.NewServer(methods, &jrpc2.ServerOptions{AllowPush: true, DisableBuiltin: true, Concurrency: 1, NewContext: func() context.Context { return ctx }}).
		Start(channel.Line(stream, stream))
	defer server.Stop()
	_ = server.Wait()
}
