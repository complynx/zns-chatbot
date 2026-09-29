package scriptworker

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

// ServeRPC handles one execution and its host callbacks over bounded stdio.
// A supervising process must kill and reap it on deadline or disconnection.
func ServeRPC(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	return serveRPCBounded(ctx, input, output, ExecuteTimeout)
}

func serveRPCBounded(ctx context.Context, input io.ReadCloser, output io.WriteCloser, timeout time.Duration) error {
	// Keep the transport alive long enough to deliver Execute's timeout response.
	// The process watchdog still bounds a stalled reader or writer.
	ctx, cancel := context.WithTimeout(ctx, timeout+scriptprotocol.ProcessWaitDelay)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = input.Close(); _ = output.Close() })
	defer stop()
	used := false
	methods := handler.Map{"execute": handler.New(func(ctx context.Context, r *jrpc2.Request) (any, error) {
		if used {
			return invalidRPCRequest()
		}
		used = true
		var request scriptprotocol.ExecuteRequest
		if len(r.ParamString()) > MaxRequestBytes || scriptprotocol.Decode([]byte(r.ParamString()), &request) != nil {
			return invalidRPCRequest()
		}
		result, err := executeBounded(
			ctx,
			request,
			func(ctx context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
				rsp, callErr := jrpc2.ServerFromContext(ctx).Callback(ctx, "tool.call", call)
				if callErr != nil {
					return nil, callErr
				}
				var value json.RawMessage
				if callErr = rsp.UnmarshalResult(&value); callErr != nil {
					return nil, callErr
				}
				return value, nil
			},
			timeout,
		)
		if err != nil {
			return Response{Error: errorCode(err)}, nil
		}
		return Response{Result: result}, nil
	})}
	stream := channel.Line(
		io.LimitReader(input, scriptprotocol.MaxTraffic),
		rpcOutput{Writer: &scriptprotocol.Writer{Output: output, Remaining: scriptprotocol.MaxTraffic}, Closer: output},
	)
	server := jrpc2.NewServer(methods, &jrpc2.ServerOptions{AllowPush: true, DisableBuiltin: true, Concurrency: 1, NewContext: func() context.Context { return ctx }}).
		Start(stream)
	defer server.Stop()
	return server.Wait()
}

type rpcOutput struct {
	io.Writer
	io.Closer
}

func invalidRPCRequest() (any, error) { return Response{Error: invalidRequestCode}, nil }
