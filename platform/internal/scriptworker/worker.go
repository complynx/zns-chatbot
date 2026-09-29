// Package scriptworker executes data-only JavaScript inside an isolated helper.
// Evaluate must never run untrusted scripts in the application process: the VM
// has an interrupt, but no hard heap limit. See docs/agent-scripting.md.
package scriptworker

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/grafana/sobek"
	"github.com/grafana/sobek/parser"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

const (
	MaxCodeBytes     = 16 * 1024
	MaxInputBytes    = 128 * 1024
	MaxOutputBytes   = 64 * 1024
	MaxResponseBytes = MaxOutputBytes + 64
	MaxRequestBytes  = 256 * 1024
	Timeout          = scriptprotocol.ActiveTime
	ProcessTimeout   = scriptprotocol.EvaluateProcessTimeout
)

var (
	ErrRequest   = errors.New("invalid script request")
	ErrExecution = errors.New("script execution failed")
	ErrResult    = errors.New("script result must be bounded JSON data")
)

type Request struct {
	Code  string          `json:"code"`
	Input json.RawMessage `json:"input"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Evaluate creates a fresh VM per request. Only JSON data crosses the boundary;
// no Go objects, credentials, application identity or host tools enter the VM.
func Evaluate(ctx context.Context, request Request) (json.RawMessage, error) {
	if len(request.Code) == 0 || len(request.Code) > MaxCodeBytes || !utf8.ValidString(request.Code) ||
		len(
			request.Input,
		) == 0 || len(request.Input) > MaxInputBytes || !utf8.Valid(request.Input) || !json.Valid(request.Input) {
		return nil, ErrRequest
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vm := sobek.New()
	// The parser's default source-map loader reads ambient filesystem paths.
	vm.SetParserOptions(parser.WithDisableSourceMaps)
	const maxStack = 256
	vm.SetMaxCallStackSize(maxStack)
	vm.SetTimeSource(func() time.Time { return time.Unix(0, 0) })
	// Scripts have no entropy source; callers must provide any required seed/data.
	vm.SetRandSource(func() float64 { return 0 })
	stop := context.AfterFunc(ctx, func() { vm.Interrupt(ErrExecution) })
	defer stop()
	// Capture the serializer and JSON parser before script code can modify globals.
	serializeValue, err := vm.RunString(serializer)
	if err != nil {
		return nil, ErrExecution
	}
	serialize, ok := sobek.AssertFunction(serializeValue)
	if !ok {
		return nil, ErrExecution
	}
	parse, ok := sobek.AssertFunction(vm.Get("JSON").ToObject(vm).Get("parse"))
	if !ok {
		return nil, ErrExecution
	}
	input, err := parse(sobek.Undefined(), vm.ToValue(string(request.Input)))
	if err != nil {
		return nil, ErrRequest
	}
	program, err := vm.RunString("(function(input) {\n\"use strict\";\n" + request.Code + "\n})")
	if err != nil {
		return nil, executionError(ctx)
	}
	run, ok := sobek.AssertFunction(program)
	if !ok {
		return nil, ErrExecution
	}
	value, err := run(sobek.Undefined(), input)
	if err != nil {
		return nil, executionError(ctx)
	}
	result, err := serialize(sobek.Undefined(), value, vm.ToValue(MaxOutputBytes))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrResult
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	data := []byte(result.String())
	if len(data) > MaxOutputBytes || !json.Valid(data) {
		return nil, ErrResult
	}
	return data, nil
}

func executionError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrExecution
}

// Captured intrinsics keep serialization independent from script mutations.
// Descriptor reads reject getters; JSON.stringify is used only for primitives.
// The process memory limit still protects allocations made by the VM or proxies.
const serializer = `(function() {
  "use strict";
  const stringify = JSON.stringify;
  const proto = Object.getPrototypeOf;
  const descriptor = Object.getOwnPropertyDescriptor;
  const keys = Reflect.ownKeys;
  const isArray = Array.isArray;
  const finite = Number.isFinite;
  const create = Object.create;
  const hasOwn = Function.prototype.call.bind(Object.prototype.hasOwnProperty);
  const objectProto = Object.prototype;
  const arrayProto = Array.prototype;
  return function(value, limit) {
    let text = "", nodes = 0;
    const parents = create(null);
    function append(part) {
      if (text.length + part.length > limit) throw 0;
      text += part;
    }
    function visit(item, depth) {
      if (++nodes > 8192 || depth > 32) throw 0;
      if (item === null) { append("null"); return; }
      const type = typeof item;
      if (type === "string") {
        if (item.length > limit) throw 0;
        append(stringify(item)); return;
      }
      if (type === "boolean" || (type === "number" && finite(item))) {
        append(stringify(item)); return;
      }
      if (type !== "object") throw 0;
      const array = isArray(item), prototype = proto(item);
      if (array ? prototype !== arrayProto : prototype !== objectProto && prototype !== null) throw 0;
      for (let i = 0; i < depth; i++) if (parents[i] === item) throw 0;
      parents[depth] = item;
      const names = keys(item);
      if (names.length > 8192) throw 0;
      append(array ? "[" : "{");
      let count = 0;
      for (let i = 0; i < names.length; i++) {
        const name = names[i];
        if (array && name === "length") continue;
        if (typeof name !== "string" || name.length > limit) throw 0;
        const property = descriptor(item, name);
        if (!property || !hasOwn(property, "value") || !property.enumerable) throw 0;
        if (array && name !== "" + count) throw 0;
        if (count++) append(",");
        if (!array) { append(stringify(name)); append(":"); }
        visit(property.value, depth + 1);
      }
      if (array && count !== descriptor(item, "length").value) throw 0;
      append(array ? "]" : "}");
      parents[depth] = null;
    }
    visit(value, 0);
    return text;
  };
})()`
