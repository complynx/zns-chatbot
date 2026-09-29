package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

const (
	codexTimeout        = 60 * time.Second
	codexOutputLimit    = 2 << 20
	codexTextField      = "text"
	codexNameField      = "name"
	codexErrorType      = "error"
	codexHostDiagnostic = "Code Mode is unavailable because code-mode host is disabled. Code mode will fail closed; enable `features.code_mode_host` and install `codex-code-mode-host`."
)

// Codex is an opt-in local synthetic-test provider using existing CLI authentication.
// SyntheticOnly is the operator's assertion; it cannot identify production data.
type Codex struct {
	Accounting    credits.Recorder
	Executable    string
	SyntheticOnly bool
}

func (m Codex) Plan(ctx context.Context, in Input) (Plan, error) {
	if !m.SyntheticOnly || !filepath.IsAbs(m.Executable) {
		return Plan{}, errors.New("Codex requires synthetic-only opt-in and an absolute executable path")
	}
	ctx, cancel := context.WithTimeout(ctx, codexTimeout)
	defer cancel()
	return planWithSkills(ctx, in, m.structured)
}

func (m Codex) structured(ctx context.Context, prompt providerPrompt) (string, error) {
	directory, err := os.MkdirTemp("", "zns-codex-")
	if err != nil {
		return "", errors.New("Codex temporary directory unavailable")
	}
	defer os.RemoveAll(directory)
	schema := filepath.Join(directory, "schema.json")
	if err = os.WriteFile(schema, []byte(prompt.schema), 0o600); err != nil {
		return "", errors.New("Codex schema unavailable")
	}
	args := codexModelArguments(ctx, directory, schema)
	args, err = codexImages(directory, args, prompt.source)
	if err != nil {
		return "", err
	}
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	//nolint:gosec // Absolute operator-configured executable; untrusted input uses stdin only.
	cmd := exec.CommandContext(ctx, m.Executable, args...)
	cmd.Dir = directory
	cmd.WaitDelay = time.Second
	cmd.Stdin = strings.NewReader(
		prompt.instructions + "\nReturn only the structured proposal. Do not use tools.\nInput JSON:\n" + string(
			prompt.input,
		),
	)
	limit := codexOutputLimit
	if prompt.name == selectionName {
		const selectionStreamBytes = 64 * 1024
		limit = selectionStreamBytes
	}
	output := &codexOutput{cancel: cancel, limit: limit}
	diagnostics := &codexOutput{cancel: cancel, limit: limit}
	cmd.Stdout, cmd.Stderr = output, diagnostics
	call, err := credits.Begin(ctx, m.Accounting, prompt.name, "codex_test", modelsettings.FromContext(ctx).Model)
	if err != nil {
		return "", err
	}
	text, resultErr := runCodexResult(ctx, parent, cmd, output, prompt.name)
	return text, errors.Join(resultErr, call.Finish(parent))
}

func runCodexResult(ctx, parent context.Context, cmd *exec.Cmd, output *codexOutput, name string) (string, error) {
	if err := cmd.Run(); err != nil {
		if parent.Err() != nil {
			return "", parent.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return "", errors.New("Codex cancelled or output limit exceeded")
		}
		return "", errors.New("Codex process failed")
	}
	text, err := readCodexText(output.Bytes())
	if err != nil {
		return "", err
	}
	if name == "zns_action_plan" {
		if err = codexRequiredFields(text); err != nil {
			return "", err
		}
	}
	return text, nil
}

func codexImages(directory string, args []string, in Input) ([]string, error) {
	images := in.Frames
	if in.Attachment != nil {
		images = []Attachment{*in.Attachment}
	}
	for index, attachment := range images {
		name := "attachment"
		if in.Attachment == nil {
			name = "frame-" + strconv.Itoa(index)
		}
		extension := ".png"
		if attachment.MIME == jpegMIME {
			extension = ".jpg"
		}
		imagePath := filepath.Join(directory, name+extension)
		if err := os.WriteFile(imagePath, attachment.Body, 0o600); err != nil {
			return nil, errors.New("Codex attachment unavailable")
		}
		args = append(args[:len(args)-1], "--image", imagePath, "-")
	}
	return args, nil
}

func codexArguments(directory, schema string) []string {
	args := []string{"exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check",
		"--model", "gpt-6-luna", "--sandbox", "read-only", "--output-schema", schema,
		"--json", "--color", "never", "--cd", directory,
		"-c", `approval_policy="never"`, "-c", `web_search="disabled"`,
		"-c", "project_doc_max_bytes=0", "-c", `shell_environment_policy.inherit="none"`}
	for _, feature := range []string{"shell_tool", "shell_snapshot", "apps", "browser_use", "browser_use_external",
		"computer_use", "in_app_browser", "image_generation", "view_image", "memories", "multi_agent", "plugins",
		"hooks", "goals", "skill_search", "skill_mcp_dependency_install", "workspace_dependencies", "tool_suggest",
		"code_mode", "code_mode_host"} {
		args = append(args, "--disable", feature)
	}
	return append(args, "-")
}

type codexOutput struct {
	bytes.Buffer

	cancel context.CancelFunc
	limit  int
}

func (w *codexOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		w.cancel()
		return 0, errors.New("Codex output exceeds budget")
	}
	return w.Buffer.Write(p)
}

type codexEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Item    struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
	} `json:"item"`
}

func readCodexText(output []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(nil, maxPlanBytes)
	var text string
	started, completed := false, false
	for scanner.Scan() {
		var event codexEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || completed {
			return "", errors.New("invalid Codex event stream")
		}
		if err := codexEventError(event, started); err != nil {
			return "", err
		}
		switch event.Type {
		case "thread.started":
			if started {
				return "", errors.New("unexpected Codex thread")
			}
		case "turn.started":
			if started {
				return "", errors.New("multiple Codex turns")
			}
			started = true
		case "turn.completed":
			completed = true
		case codexErrorType:
		case "item.completed":
			next, err := codexMessage(event, started, text)
			if err != nil {
				return "", err
			}
			text = next
		default:
			return "", errors.New("unexpected Codex event; tools are forbidden")
		}
	}
	if scanner.Err() != nil || !completed || !started || text == "" {
		return "", errors.New("incomplete Codex response")
	}
	return text, nil
}

func codexEventError(event codexEvent, started bool) error {
	if event.Type == codexErrorType && (!started || !codexReconnect(event.Message)) {
		return errors.New("unexpected Codex error")
	}
	return nil
}

// The CLI can recover a disconnected stream within the same turn. Accept only
// this observed transport diagnostic; a completed, valid final plan is still required.
func codexReconnect(message string) bool {
	for attempt := range 5 {
		if message == "Reconnecting... "+strconv.Itoa(attempt+1)+
			"/5 (stream disconnected before completion: websocket closed by server before response.completed)" {
			return true
		}
	}
	return false
}

func codexMessage(event codexEvent, started bool, current string) (string, error) {
	if event.Item.Type == codexErrorType && !started && event.Item.Message == codexHostDiagnostic {
		return current, nil
	}
	if event.Item.Type != "agent_message" || !started || current != "" {
		return "", errors.New("unexpected Codex item; tools are forbidden")
	}
	return event.Item.Text, nil
}

// Require schema keys explicitly; Go's decoder otherwise accepts absent/null strings.
func codexRequiredFields(text string) error {
	fields, err := codexObject([]byte(text))
	if err != nil || (len(fields) != 10 && len(fields) != 11) {
		return errors.New("invalid Codex plan")
	}
	for _, name := range []string{codexTextField, planViewField, "action", "order_action", "profile_action", "media_action", planKnowledgeField, planScriptField, planHistoryField, planRegistrationField} {
		value, ok := fields[name]
		if !ok || ((name == codexTextField || name == planViewField) && bytes.Equal(value, []byte("null"))) {
			return errors.New("missing Codex plan field")
		}
	}
	for name, required := range map[string][]string{
		"action": {codexNameField, "slot_id"}, "order_action": {codexNameField, "order_id", "extra"},
		"profile_action": {codexNameField, "field", "value"},
		planScriptField:  {"code", "input_json"},
	} {
		if bytes.Equal(fields[name], []byte("null")) {
			continue
		}
		if err = codexStringFields(fields[name], required); err != nil {
			return err
		}
	}
	return codexComplexActions(fields)
}

func codexComplexActions(fields map[string]json.RawMessage) error {
	for name, validate := range map[string]func([]byte) error{
		"media_action":        codexMediaFields,
		planKnowledgeField:    codexKnowledgeFields,
		planHistoryField:      codexHistoryFields,
		planRegistrationField: codexRegistrationFields,
		planLineupField:       codexLineupFields,
	} {
		if len(fields[name]) > 0 && !bytes.Equal(fields[name], []byte("null")) {
			if err := validate(fields[name]); err != nil {
				return err
			}
		}
	}
	return nil
}
func codexMediaFields(data []byte) error {
	fields, err := codexObject(data)
	if raw, present := fields["food_kind"]; present {
		var kind string
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &kind) != nil {
			return errors.New("invalid Codex food kind")
		}
		delete(fields, "food_kind")
	}
	if err != nil || len(fields) != 9 {
		return errors.New("invalid Codex media action")
	}
	for _, name := range []string{"media_id", "intent", "amount", "currency", "order_id", mediaRegistrationField} {
		var value string
		if bytes.Equal(fields[name], []byte("null")) || json.Unmarshal(fields[name], &value) != nil {
			return errors.New("invalid Codex media string")
		}
	}
	for _, name := range []string{"start_ms", "end_ms", "frame_count"} {
		var value int64
		if bytes.Equal(fields[name], []byte("null")) || json.Unmarshal(fields[name], &value) != nil {
			return errors.New("invalid Codex media number")
		}
	}
	return nil
}

func codexStringFields(data []byte, required []string) error {
	fields, err := codexObject(data)
	if err != nil || len(fields) != len(required) {
		return errors.New("invalid Codex action")
	}
	for _, name := range required {
		value, ok := fields[name]
		var text string
		if !ok || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &text) != nil {
			return errors.New("missing Codex action field")
		}
	}
	return nil
}

// Decode object keys explicitly to reject duplicate keys and case aliases.
func codexObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("invalid Codex object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		key, ok := token.(string)
		if tokenErr != nil || !ok {
			return nil, errors.New("invalid Codex key")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate Codex key")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, errors.New("invalid Codex value")
		}
		fields[key] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, errors.New("invalid Codex object end")
	}
	var tail any
	if !errors.Is(decoder.Decode(&tail), io.EOF) {
		return nil, errors.New("trailing Codex plan data")
	}
	return fields, nil
}
