package scriptworker

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

func TestRPCHostTimeoutResponse(t *testing.T) {
	t.Parallel()
	clientStream, workerStream := net.Pipe()
	t.Cleanup(func() { _ = clientStream.Close(); _ = workerStream.Close() })
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = serveRPCBounded(t.Context(), workerStream, workerStream, 100*time.Millisecond)
	}()
	client := jrpc2.NewClient(channel.Line(clientStream, clientStream), &jrpc2.ClientOptions{
		OnCallback: handler.New(func(ctx context.Context, _ *jrpc2.Request) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	})
	t.Cleanup(func() { _ = client.Close(); <-workerDone })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var response Response
	err := client.CallResult(ctx, "execute", scriptprotocol.ExecuteRequest{
		Code: `return await tools.read({});`, Input: json.RawMessage(`null`),
		Tools: []scriptprotocol.Tool{{Name: "read"}},
	}, &response)
	require.NoError(t, err)
	require.NoError(t, ctx.Err())
	require.Equal(t, "timeout", response.Error)
}
