package scriptworker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func evaluate(t *testing.T, code string) (json.RawMessage, error) {
	t.Helper()
	return scriptworker.Evaluate(t.Context(), scriptworker.Request{Code: code, Input: json.RawMessage(`null`)})
}

func TestStructuredTransformAndFreshRuntime(t *testing.T) {
	t.Parallel()
	request := scriptworker.Request{
		Code: `globalThis.saved = (globalThis.saved || 0) + 1;
return {total: input.items.reduce((sum, item) => sum + item.price, 0), saved, at: Date.now(), random: Math.random()};`,
		Input: json.RawMessage(`{"items":[{"price":2},{"price":3}]}`),
	}
	first, err := scriptworker.Evaluate(t.Context(), request)
	require.NoError(t, err)
	second, err := scriptworker.Evaluate(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.JSONEq(t, `{"total":5,"saved":1,"at":0,"random":0}`, string(first))
}

func TestScriptCannotReachHostCapabilities(t *testing.T) {
	t.Parallel()
	data, err := evaluate(t, `return [typeof process, typeof require, typeof fetch, typeof XMLHttpRequest,
typeof WebSocket, typeof Deno, typeof Bun, typeof console, typeof setTimeout,
typeof readFile, typeof writeFile, typeof os, typeof exec, typeof host, typeof tools];`)
	require.NoError(t, err)
	var result []string
	require.NoError(t, json.Unmarshal(data, &result))
	for _, value := range result {
		assert.Equal(t, "undefined", value)
	}
}

func TestScriptInterruptAndCancel(t *testing.T) {
	t.Parallel()
	started := time.Now()
	_, err := evaluate(t, `while (true) {}`)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), scriptworker.ProcessTimeout)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = scriptworker.Evaluate(ctx, scriptworker.Request{Code: `return 1;`, Input: json.RawMessage(`null`)})
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	_, err = scriptworker.Evaluate(ctx, scriptworker.Request{Code: `while (true) {}`, Input: json.RawMessage(`null`)})
	require.ErrorIs(t, err, context.Canceled)
}

func TestInvalidScriptResults(t *testing.T) {
	t.Parallel()
	for name, code := range map[string]string{
		"undefined": `return undefined;`, "function": `return () => 1;`,
		"symbol": `return Symbol("x");`, "bigint": `return 1n;`,
		"nan": `return NaN;`, "infinity": `return Infinity;`,
		"nested undefined": `return {x: undefined};`, "cycle": `const a = {}; a.a = a; return a;`,
		"date": `return new Date();`, "promise": `return Promise.resolve(1);`,
		"getter": `return {get x() {while(true) {}}};`, "hole": `return new Array(3);`,
		"huge output": `return "x".repeat(70000);`, "huge utf8": `return "€".repeat(30000);`,
		"many nodes": `return Array(10000).fill(1);`, "deep": `let a = null; for(let i=0;i<40;i++) a={a}; return a;`,
		"toJSON":           `return {toJSON() {return 1;}};`,
		"prototype poison": `Object.prototype.value = 1; return {get x() {return 2;}};`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := evaluate(t, code)
			require.ErrorIs(t, err, scriptworker.ErrResult)
		})
	}
}

func TestSerializerSurvivesIntrinsicMutation(t *testing.T) {
	t.Parallel()
	data, err := evaluate(t, `JSON.stringify = () => "oops"; Object.getOwnPropertyDescriptor = () => null;
Object.getPrototypeOf = () => null; Reflect.ownKeys = () => []; Array.prototype.push = () => {};
return {ok: [1, true, "text", null]};`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":[1,true,"text",null]}`, string(data))
}

func TestScriptProtocolBoundsAndErrors(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"unknown":          `{"code":"return 1;","input":null,"actor":1}`,
		"trailing":         `{"code":"return 1;","input":null} {}`,
		"missing input":    `{"code":"return 1;"}`,
		"code type":        `{"code":3,"input":null}`,
		"oversize request": strings.Repeat(" ", scriptworker.MaxRequestBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			require.NoError(t, scriptworker.Serve(t.Context(), strings.NewReader(input), &output))
			assert.JSONEq(t, `{"error":"invalid_request"}`, output.String())
		})
	}
	var output bytes.Buffer
	require.NoError(
		t,
		scriptworker.Serve(t.Context(), strings.NewReader(`{"code":"return input.x + 1;","input":{"x":2}}`), &output),
	)
	assert.JSONEq(t, `{"result":3}`, output.String())
	_, err := evaluate(t, `throw new Error("private-text");`)
	require.ErrorIs(t, err, scriptworker.ErrExecution)
	assert.NotContains(t, err.Error(), "private-text")
	_, err = evaluate(t, strings.Repeat(" ", scriptworker.MaxCodeBytes+1))
	require.ErrorIs(t, err, scriptworker.ErrRequest)
	_, err = scriptworker.Evaluate(
		t.Context(),
		scriptworker.Request{
			Code:  `return input;`,
			Input: json.RawMessage(`"` + strings.Repeat("x", scriptworker.MaxInputBytes) + `"`),
		},
	)
	require.ErrorIs(t, err, scriptworker.ErrRequest)
}

func TestResponseEnvelopePreservesOutputBudget(t *testing.T) {
	t.Parallel()
	request := `{"code":"return '<&>'.repeat(21000);","input":null}`
	var output bytes.Buffer
	require.NoError(t, scriptworker.Serve(t.Context(), strings.NewReader(request), &output))
	assert.LessOrEqual(t, output.Len(), scriptworker.MaxResponseBytes)
	var response scriptworker.Response
	require.NoError(t, json.Unmarshal(output.Bytes(), &response))
	assert.Empty(t, response.Error)
	assert.LessOrEqual(t, len(response.Result), scriptworker.MaxOutputBytes)
}
