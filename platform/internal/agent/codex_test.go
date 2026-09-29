package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// The test binary acts as the CLI, so tests exercise real pipes and process cleanup.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "exec" {
		os.Exit(codexHelper())
	}
	os.Exit(m.Run())
}

func codexHelper() int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 1
	}
	parts := strings.SplitN(string(data), "Input JSON:\n", 2)
	if len(parts) != 2 {
		return 1
	}
	var in agent.Input
	contextJSON, _, _ := strings.Cut(parts[1], "\nCurrent user utterance (untrusted JSON string):\n")
	if json.Unmarshal([]byte(contextJSON), &in) != nil {
		return 1
	}
	args := strings.Join(os.Args[1:], " ")
	for _, required := range []string{"--model gpt-6-luna", "--ignore-user-config", "--ephemeral",
		"--sandbox read-only", `approval_policy="never"`, `web_search="disabled"`,
		"--disable plugins", "--disable hooks", "--disable memories", "--disable shell_tool", "--disable code_mode_host"} {
		if !strings.Contains(args, required) {
			return 1
		}
	}
	if !codexHelperFiles(in) {
		return 1
	}
	schema, err := os.ReadFile("schema.json")
	if err != nil {
		return 1
	}
	if in.Text == "capability-visibility" {
		return codexCapabilityHelper(string(data), schema)
	}
	if bytes.Contains(schema, []byte(`"skills"`)) {
		return codexHelperSelection(in.Text)
	}
	if in.Text == "selected-profile" {
		if !strings.Contains(parts[0], "profile.pending is a hint") ||
			strings.Contains(parts[0], "Video frames carry") {
			return 1
		}
		in.Text = codexStream(
			`{"text":"Profile skill loaded","view":"profile","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`,
		)
	}
	if in.Text == "wait" {
		time.Sleep(time.Minute)
		return 1
	}
	if in.Text == "overflow" {
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 3<<20))
		return 1
	}
	if in.Text == "cwd" {
		dir, cwdErr := os.Getwd()
		if cwdErr != nil {
			return 1
		}
		in.Text = codexStream(
			`{"text":` + strconvJSON(
				dir,
			) + `,"view":"orders","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`,
		)
	}
	_, err = os.Stdout.WriteString(in.Text)
	if err != nil {
		return 1
	}
	return 0
}

func codexHelperSelection(text string) int {
	selection := `{"skills":[],"reply_language":"en"}`
	if text == "selected-profile" {
		selection = `{"skills":["profile"],"reply_language":"en"}`
	}
	_, err := os.Stdout.WriteString(codexStream(selection))
	if err != nil {
		return 1
	}
	return 0
}

func codexHelperFiles(in agent.Input) bool {
	entries, err := os.ReadDir(".")
	if err != nil {
		return false
	}
	switch {
	case in.Attachment != nil:
		return len(entries) == 2 && codexHelperImage(in.Attachment)
	case len(in.Frames) > 0:
		return len(entries) == len(in.Frames)+1 && codexHelperFrames(in.Frames)
	default:
		return len(entries) == 1 && entries[0].Name() == "schema.json"
	}
}

func codexHelperFrames(frames []agent.Attachment) bool {
	index := 0
	for pos, arg := range os.Args {
		if arg != "--image" {
			continue
		}
		if pos+1 >= len(os.Args) || index >= len(frames) || len(frames[index].Body) != 0 {
			return false
		}
		body, err := os.ReadFile(os.Args[pos+1])
		if err != nil || !bytes.Equal(body, testImage()) {
			return false
		}
		index++
	}
	return index == len(frames)
}

func TestCodexSelectsOnlyRequestedSkill(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	plan, err := (agent.Codex{Executable: executable, SyntheticOnly: true}).Plan(
		t.Context(),
		agent.Input{Text: "selected-profile"},
	)
	require.NoError(t, err)
	assert.Equal(t, "Profile skill loaded", plan.Text)
}

func TestCodexFrames(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	frame := agent.Attachment{ID: "video", MIME: "image/png", Body: testImage(), TimestampMS: 1000}
	plan, err := (agent.Codex{Executable: executable, SyntheticOnly: true}).Plan(t.Context(), agent.Input{
		Text: "cwd", Frames: []agent.Attachment{frame, frame},
	})
	require.NoError(t, err)
	_, err = os.Stat(plan.Text)
	assert.True(t, os.IsNotExist(err), "temporary frame directory must be removed")
}

func codexHelperImage(attachment *agent.Attachment) bool {
	if len(attachment.Body) != 0 {
		return false
	}
	args := os.Args
	if len(args) < 4 || args[len(args)-3] != "--image" || args[len(args)-1] != "-" {
		return false
	}
	path := args[len(args)-2]
	directory, err := os.Getwd()
	if err != nil || filepath.Dir(path) != directory || filepath.Base(path) != "attachment.png" {
		return false
	}
	body, err := os.ReadFile(path)
	return err == nil && bytes.Equal(body, testImage())
}

func TestCodexImage(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	attachment := &agent.Attachment{ID: "synthetic", Filename: "--evil.jpg", MIME: "image/png", Body: testImage()}
	plan, err := (agent.Codex{Executable: executable, SyntheticOnly: true}).Plan(t.Context(), agent.Input{
		Text: "cwd", Attachment: attachment,
	})
	require.NoError(t, err)
	_, err = os.Stat(plan.Text)
	assert.True(t, os.IsNotExist(err), "temporary image directory must be removed")
}

func strconvJSON(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func codexStream(plan string) string {
	return "{\"type\":\"thread.started\"}\n{\"type\":\"turn.started\"}\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":` + strconvJSON(plan) + "}}\n" +
		"{\"type\":\"turn.completed\"}\n"
}

func TestCodexContract(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	good := `{"text":"Proposed only","view":"orders","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`
	diagnostic := "{\"type\":\"item.completed\",\"item\":{\"type\":\"error\",\"message\":" + strconvJSON(
		"Code Mode is unavailable because code-mode host is disabled. Code mode will fail closed; enable `features.code_mode_host` and install `codex-code-mode-host`.",
	) + "}}\n"
	reconnect := `{"type":"error","message":"Reconnecting... 2/5 (stream disconnected before completion: websocket closed by server before response.completed)"}` + "\n"
	recovered := strings.Replace(
		codexStream(good),
		"{\"type\":\"turn.started\"}\n",
		"{\"type\":\"turn.started\"}\n"+reconnect,
		1,
	)
	for _, tc := range []struct {
		name, stream string
		ok           bool
	}{
		{"valid", codexStream(good), true},
		{"reconnected", recovered, true},
		{"reconnect_before_turn", reconnect + codexStream(good), false},
		{"reconnect_incomplete", strings.ReplaceAll(recovered, `{"type":"turn.completed"}`, ""), false},
		{"reconnect_then_tool", strings.ReplaceAll(recovered, "agent_message", "command_execution"), false},
		{"reconnect_unknown_error", strings.ReplaceAll(recovered, "Reconnecting... 2/5", "Unrecognized diagnostic"), false},
		{"media", codexStream(`{"text":"Evidence","view":"media","action":null,"order_action":null,"profile_action":null,"media_action":{"media_id":"image","intent":"receipt","amount":"80.00","currency":"BYN","order_id":"","registration_event":"","start_ms":0,"end_ms":0,"frame_count":0},"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`), true},
		{"missing_media", codexStream(strings.Replace(good, `,"media_action":null`, "", 1)), false},
		{"media_missing_fields", codexStream(strings.Replace(good, `"media_action":null`, `"media_action":{"media_id":"image","intent":"receipt"}`, 1)), false},
		{"video", codexStream(`{"text":"Inspect","view":"media","action":null,"order_action":null,"profile_action":null,"media_action":{"media_id":"video","intent":"inspect_video","amount":"","currency":"","order_id":"","registration_event":"","start_ms":1000,"end_ms":5000,"frame_count":4},"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`), true},
		{"profile", codexStream(`{"text":"Proposed","view":"profile","action":null,"order_action":null,"profile_action":{"name":"set","field":"legal_name","value":"John Smith"},"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`), true},
		{"known_startup", diagnostic + codexStream(good), true},
		{"late_diagnostic", strings.Replace(codexStream(good), "{\"type\":\"turn.started\"}\n", "{\"type\":\"turn.started\"}\n"+diagnostic, 1), false},
		{"duplicate_key", codexStream(strings.TrimSuffix(good, "}") + `,"view":"workflow"}`), false},
		{"case_alias", codexStream(strings.TrimSuffix(good, "}") + `,"View":"workflow"}`), false},
		{"missing_action_fields", codexStream(`{"text":"x","view":"orders","action":null,"order_action":{"name":"create"}}`), false},
		{"null_action_field", codexStream(`{"text":"x","view":"orders","action":null,"order_action":{"name":"create","order_id":null,"extra":""}}`), false},
		{"missing_fields", codexStream(`{"view":"orders"}`), false},
		{"unknown_fields", codexStream(strings.TrimSuffix(good, "}") + `,"principal":"admin"}`), false},
		{"forbidden", codexStream(`{"text":"x","view":"workflow","action":{"name":"confirm","slot_id":"x"},"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`), false},
		{"foreign_slot", codexStream(`{"text":"x","view":"workflow","action":{"name":"select","slot_id":"x"},"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`), false},
		{"trailing_plan", codexStream(good + ` {}`), false},
		{"truncated", strings.ReplaceAll(codexStream(good), `{"type":"turn.completed"}`, ""), false},
		{"failed", strings.ReplaceAll(codexStream(good), "turn.completed", "turn.failed"), false},
		{"tool", strings.ReplaceAll(codexStream(good), "agent_message", "command_execution"), false},
		{"tool_start", "{\"type\":\"item.started\",\"item\":{\"type\":\"mcp_tool_call\"}}\n" + codexStream(good), false},
		{"extra_turn", codexStream(good) + codexStream(good), false},
		{"arbitrary_error", "{\"type\":\"error\",\"message\":\"no\"}\n" + codexStream(good), false},
		{"overflow", "overflow", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, planErr := (agent.Codex{Executable: executable, SyntheticOnly: true}).Plan(
				t.Context(),
				agent.Input{Text: tc.stream},
			)
			if tc.ok {
				require.NoError(t, planErr)
			} else {
				require.Error(t, planErr)
			}
		})
	}
}

func TestCodexBoundsAndCleanup(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	model := agent.Codex{Executable: executable, SyntheticOnly: true}
	plan, err := model.Plan(t.Context(), agent.Input{Text: "cwd"})
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(plan.Text))
	_, err = os.Stat(plan.Text)
	assert.True(t, os.IsNotExist(err))
	_, err = model.Plan(t.Context(), agent.Input{Text: strings.Repeat("x", 60001)})
	require.Error(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = model.Plan(ctx, agent.Input{Text: "wait"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	_, err = (agent.Codex{Executable: executable}).Plan(t.Context(), agent.Input{})
	require.Error(t, err)
	_, err = (agent.Codex{Executable: "codex", SyntheticOnly: true}).Plan(t.Context(), agent.Input{})
	require.Error(t, err)
}
