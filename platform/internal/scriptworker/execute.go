package scriptworker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grafana/sobek"
	"github.com/grafana/sobek/parser"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

const (
	ExecuteTimeout        = 5 * time.Second
	ExecuteProcessTimeout = ExecuteTimeout + time.Second
)

type execution struct {
	ctx         context.Context
	vm          *sobek.Runtime
	serialize   sobek.Callable
	parse       sobek.Callable
	callback    scriptprotocol.Callback
	timer       *time.Timer
	remaining   time.Duration
	started     time.Time
	calls       int
	discoveries int
	unhandled   map[*sobek.Promise]struct{}
}

// Execute exposes only supplied host tools in a fresh isolated VM. Host calls
// are sequential; native promise jobs allow await on immediately returned data.
// No timers, module loading or general asynchronous IO are installed.
func Execute(
	ctx context.Context,
	request scriptprotocol.ExecuteRequest,
	callback scriptprotocol.Callback,
) (json.RawMessage, error) {
	if !validExecuteRequest(request, callback) {
		return nil, ErrRequest
	}
	ctx, cancel := context.WithTimeout(ctx, ExecuteTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vm := sobek.New()
	// This applies to initial parsing and scripts compiled by eval or Function.
	vm.SetParserOptions(parser.WithDisableSourceMaps)
	const stackLimit = 256
	vm.SetMaxCallStackSize(stackLimit)
	vm.SetTimeSource(func() time.Time { return time.Unix(0, 0) })
	vm.SetRandSource(func() float64 { return 0 })
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ctx.Err()) })
	defer stop()
	e := &execution{
		ctx: ctx, vm: vm, callback: callback, remaining: Timeout, started: time.Now(),
		unhandled: make(map[*sobek.Promise]struct{}),
	}
	vm.SetPromiseRejectionTracker(e.trackRejection)
	e.timer = time.AfterFunc(Timeout, func() { vm.Interrupt(context.DeadlineExceeded) })
	defer e.timer.Stop()
	if err := e.initialize(); err != nil {
		return nil, err
	}
	tools, err := e.bindTools(request.Tools)
	if err != nil {
		return nil, err
	}
	return e.run(request, tools)
}

// Sobek drains native promise jobs when a VM call returns. Retain only rejected
// promises that still lack a handler, including jobs created during serialization.
func (e *execution) trackRejection(promise *sobek.Promise, operation sobek.PromiseRejectionOperation) {
	switch operation {
	case sobek.PromiseRejectionReject:
		e.unhandled[promise] = struct{}{}
	case sobek.PromiseRejectionHandle:
		delete(e.unhandled, promise)
	}
}

func validExecuteRequest(r scriptprotocol.ExecuteRequest, callback scriptprotocol.Callback) bool {
	return len(r.Code) > 0 && len(r.Code) <= MaxCodeBytes && utf8.ValidString(r.Code) && len(r.Input) > 0 &&
		len(r.Input) <= MaxInputBytes &&
		utf8.Valid(r.Input) &&
		json.Valid(r.Input) &&
		scriptprotocol.ValidateTools(r.Tools) == nil &&
		(len(r.Tools) == 0 || callback != nil)
}

func (e *execution) initialize() error {
	value, err := e.vm.RunString(serializer)
	if err != nil {
		return ErrExecution
	}
	e.serialize, _ = sobek.AssertFunction(value)
	e.parse, _ = sobek.AssertFunction(e.vm.Get("JSON").ToObject(e.vm).Get("parse"))
	return nil
}

func (e *execution) bindTools(descriptors []scriptprotocol.Tool) (*sobek.Object, error) {
	root := e.vm.NewObject()
	if err := root.SetPrototype(nil); err != nil {
		return nil, ErrExecution
	}
	if err := root.Set("$list", func() sobek.Value {
		if e.callback == nil {
			return e.parseJSON(json.RawMessage(`[]`))
		}
		return e.callHost(scriptprotocol.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)}, true)
	}); err != nil {
		return nil, ErrExecution
	}
	for _, tool := range descriptors {
		if err := e.bindTool(root, tool); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func (e *execution) bindTool(root *sobek.Object, tool scriptprotocol.Tool) error {
	function := e.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		argument := call.Argument(0)
		if len(call.Arguments) == 0 {
			// Bindings carry names only. The host still validates required fields
			// and permissions; omission is merely the ergonomic spelling of {}.
			argument = e.vm.NewObject()
		}
		args, err := e.serialize(sobek.Undefined(), argument, e.vm.ToValue(scriptprotocol.MaxArguments))
		if err != nil || !scriptprotocol.ValidArguments(json.RawMessage(args.String())) {
			panic(e.vm.NewTypeError("invalid tool arguments"))
		}
		return e.callHost(scriptprotocol.ToolCall{Name: tool.Name, Arguments: json.RawMessage(args.String())}, false)
	}).ToObject(e.vm)
	args, err := json.Marshal(map[string]string{"name": tool.Name})
	if err != nil {
		return ErrRequest
	}
	if err = function.Set(
		"$help",
		func() sobek.Value { return e.callHost(scriptprotocol.ToolCall{Name: "$help", Arguments: args}, true) },
	); err != nil {
		return ErrExecution
	}
	parts := strings.Split(tool.Name, ".")
	parent, err := e.namespace(root, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	if err = parent.Set(parts[len(parts)-1], function); err != nil {
		return ErrExecution
	}
	return nil
}

func (e *execution) namespace(root *sobek.Object, parts []string) (*sobek.Object, error) {
	parent := root
	for _, part := range parts {
		value := parent.Get(part)
		if value != nil && !sobek.IsUndefined(value) {
			parent = value.ToObject(e.vm)
			continue
		}
		child := e.vm.NewObject()
		if err := child.SetPrototype(nil); err != nil {
			return nil, ErrExecution
		}
		if err := parent.Set(part, child); err != nil {
			return nil, ErrExecution
		}
		parent = child
	}
	return parent, nil
}

func (e *execution) callHost(call scriptprotocol.ToolCall, discovery bool) sobek.Value {
	if discovery {
		if e.discoveries >= scriptprotocol.MaxDiscoveries {
			panic(e.vm.NewTypeError("script discovery limit exceeded"))
		}
		e.discoveries++
	} else {
		if e.calls >= scriptprotocol.MaxCalls {
			panic(e.vm.NewTypeError("script call limit exceeded"))
		}
		e.calls++
	}
	// Pause cumulative JS time only while the host waits on bounded application IO.
	e.timer.Stop()
	e.remaining -= time.Since(e.started)
	if e.remaining <= 0 {
		e.vm.Interrupt(context.DeadlineExceeded)
		return sobek.Undefined()
	}
	result, err := e.callback(e.ctx, call)
	e.started = time.Now()
	e.timer.Reset(e.remaining)
	if e.ctx.Err() != nil {
		e.vm.Interrupt(e.ctx.Err())
		return sobek.Undefined()
	}
	if err != nil {
		panic(e.vm.NewTypeError("tool call failed"))
	}
	if len(result) > scriptprotocol.MaxResult || !utf8.Valid(result) || !json.Valid(result) {
		panic(e.vm.NewTypeError("invalid tool result"))
	}
	return e.parseJSON(result)
}

func (e *execution) parseJSON(data json.RawMessage) sobek.Value {
	value, err := e.parse(sobek.Undefined(), e.vm.ToValue(string(data)))
	if err != nil {
		panic(e.vm.NewTypeError("invalid tool result"))
	}
	return value
}

func (e *execution) run(request scriptprotocol.ExecuteRequest, tools *sobek.Object) (json.RawMessage, error) {
	input := e.parseJSON(request.Input)
	program, err := e.vm.RunString("(async function(input, tools) {\n\"use strict\";\n" + request.Code + "\n})")
	if err != nil {
		return nil, executeError(e.ctx, err)
	}
	run, _ := sobek.AssertFunction(program)
	value, err := run(sobek.Undefined(), input, tools)
	if err != nil {
		return nil, executeError(e.ctx, err)
	}
	promise, ok := value.Export().(*sobek.Promise)
	if !ok || promise.State() != sobek.PromiseStateFulfilled {
		return nil, ErrExecution
	}
	result, err := e.serialize(sobek.Undefined(), promise.Result(), e.vm.ToValue(MaxOutputBytes))
	if err != nil {
		if _, interrupted := errors.AsType[*sobek.InterruptedError](err); interrupted {
			return nil, executeError(e.ctx, err)
		}
		return nil, ErrResult
	}
	if e.ctx.Err() != nil {
		return nil, e.ctx.Err()
	}
	if len(e.unhandled) != 0 {
		return nil, ErrExecution
	}
	data := json.RawMessage(result.String())
	if len(data) > MaxOutputBytes || !json.Valid(data) {
		return nil, ErrResult
	}
	return data, nil
}

func executeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, interrupted := errors.AsType[*sobek.InterruptedError](err); interrupted {
		return context.DeadlineExceeded
	}
	return ErrExecution
}
