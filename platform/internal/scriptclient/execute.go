package scriptclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

const executeDeadline = 7 * time.Second

type Tool = scriptprotocol.Tool
type ToolCall = scriptprotocol.ToolCall
type Callback = scriptprotocol.Callback

// Execute starts one bounded isolated run. callback must apply the same host
// authorization, validation and durable mutation receipts as direct tool calls.
// It receives the original run context, never worker-supplied actor metadata.
func (c *Client) Execute(
	ctx context.Context,
	request Request,
	tools []Tool,
	callback Callback,
) (json.RawMessage, error) {
	if len(request.Code) == 0 || len(request.Code) > maxCode || !utf8.ValidString(request.Code) ||
		len(request.Input) == 0 ||
		len(request.Input) > maxInput ||
		!utf8.Valid(request.Input) ||
		!json.Valid(request.Input) ||
		scriptprotocol.ValidateTools(tools) != nil ||
		(len(tools) > 0 && callback == nil) {
		return nil, errors.New("invalid script input")
	}
	ctx, cancel := context.WithTimeout(ctx, executeDeadline)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = connection.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err = io.WriteString(connection, "CONNECT /execute HTTP/1.1\r\nHost: script\r\n\r\n"); err != nil {
		return nil, err
	}
	// Bound handshake and all RPC frames before their parsers allocate buffers.
	reader := bufio.NewReader(io.LimitReader(connection, scriptprotocol.MaxTraffic))
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, errors.New("script worker unavailable")
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, errors.New("script worker rejected request")
	}
	stream := channel.Line(
		reader,
		executeOutput{
			Writer: &scriptprotocol.Writer{Output: connection, Remaining: scriptprotocol.MaxTraffic},
			Closer: connection,
		},
	)
	allowed := make(map[string]bool, len(tools))
	for _, tool := range tools {
		allowed[tool.Name] = true
	}
	dispatch := &callbackDispatch{ctx: ctx, allowed: allowed, callback: callback}
	client := jrpc2.NewClient(stream, &jrpc2.ClientOptions{OnCallback: handler.New(dispatch.call)})
	defer client.Close()
	var result json.RawMessage
	err = client.CallResult(
		ctx,
		"execute",
		scriptprotocol.ExecuteRequest{Code: request.Code, Input: request.Input, Tools: tools},
		&result,
	)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("script execution failed")
	}
	if len(result) > maxResponse {
		return nil, ErrResultLimit
	}
	return decode(result)
}

type executeOutput struct {
	io.Writer
	io.Closer
}

type callbackDispatch struct {
	lock        sync.Mutex
	ctx         context.Context
	allowed     map[string]bool
	callback    Callback
	calls       int
	discoveries int
}

func (d *callbackDispatch) call(_ context.Context, r *jrpc2.Request) (any, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	var call ToolCall
	if d.ctx.Err() != nil || r.Method() != "tool.call" || r.IsNotification() ||
		len(r.ParamString()) > scriptprotocol.MaxArguments+256 ||
		scriptprotocol.Decode([]byte(r.ParamString()), &call) != nil ||
		!scriptprotocol.ValidArguments(call.Arguments) ||
		d.callback == nil ||
		!d.admit(call) {
		return nil, jrpc2.Errorf(jrpc2.InvalidParams, "tool call rejected")
	}
	result, err := d.callback(d.ctx, call)
	if err != nil || d.ctx.Err() != nil {
		return nil, jrpc2.Errorf(jrpc2.InternalError, "tool call failed")
	}
	if len(result) > scriptprotocol.MaxResult || !utf8.Valid(result) || !json.Valid(result) {
		return nil, jrpc2.Errorf(jrpc2.InternalError, "invalid tool result")
	}
	return result, nil
}

func (d *callbackDispatch) admit(call ToolCall) bool {
	if call.Name == "$list" || call.Name == "$help" {
		if d.discoveries >= scriptprotocol.MaxDiscoveries {
			return false
		}
		if call.Name == "$help" {
			var target struct {
				Name string `json:"name"`
			}
			if scriptprotocol.Decode(call.Arguments, &target) != nil || !d.allowed[target.Name] {
				return false
			}
		}
		d.discoveries++
		return true
	}
	if !d.allowed[call.Name] || d.calls >= scriptprotocol.MaxCalls {
		return false
	}
	d.calls++
	return true
}
