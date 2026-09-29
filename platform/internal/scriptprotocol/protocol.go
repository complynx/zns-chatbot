// Package scriptprotocol defines the bounded data crossing the script boundary.
// It contains no JavaScript engine, application identity or business authority.
package scriptprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const (
	// MaxTools bounds bindings; 128 maximum-length names fit within the 64 KiB catalog budget.
	MaxTools       = 128
	MaxDiscoveries = 32
	MaxCalls       = 8
	MaxArguments   = 128 << 10
	MaxResult      = 64 << 10
	MaxTraffic     = 512 << 10
)

type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	ResultSchema json.RawMessage `json:"result_schema,omitempty"`
	Example      string          `json:"example,omitempty"`
}

type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Callback executes under the host's captured authenticated run context. The
// host must durably reserve mutations before applying them; a script failure
// cannot roll back a completed callback.
type Callback func(context.Context, ToolCall) (json.RawMessage, error)

type ExecuteRequest struct {
	Code  string          `json:"code"`
	Input json.RawMessage `json:"input"`
	Tools []Tool          `json:"tools"`
}

// ValidateTools rejects names that cannot be safely projected by the library.
func ValidateTools(tools []Tool) error {
	if len(tools) > MaxTools {
		return errors.New("too many script tools")
	}
	valid := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if len(tool.Name) > 128 || seen[tool.Name] || len(tool.Description) > 1024 || len(tool.Example) > 2048 ||
			len(tool.InputSchema) > 8192 ||
			len(tool.ResultSchema) > 8192 ||
			(len(tool.InputSchema) > 0 && !ValidSchema(tool.InputSchema)) ||
			(len(tool.ResultSchema) > 0 && !ValidSchema(tool.ResultSchema)) {
			return errors.New("invalid script tool")
		}
		if !validToolName(tool.Name, valid) {
			return errors.New("invalid script tool")
		}
		for name := range seen {
			if strings.HasPrefix(name, tool.Name+".") || strings.HasPrefix(tool.Name, name+".") {
				return errors.New("conflicting script tool")
			}
		}
		seen[tool.Name] = true
	}
	data, err := json.Marshal(tools)
	if err != nil || len(data) > 64<<10 {
		return errors.New("script catalog exceeds limit")
	}
	return nil
}

func ValidSchema(data json.RawMessage) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && data[0] == '{' && json.Valid(data)
}

func ValidArguments(data json.RawMessage) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && len(data) <= MaxArguments && data[0] == '{' && json.Valid(data)
}

// Decode rejects fields outside the typed wire contract and trailing values.
func Decode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return errors.New("trailing script data")
	}
	return nil
}

// Writer caps the whole per-run stream, including protocol overhead.
type Writer struct {
	Output    io.Writer
	Remaining int64
}

func (w *Writer) Write(data []byte) (int, error) {
	if int64(len(data)) > w.Remaining {
		return 0, errors.New("script traffic exceeded")
	}
	n, err := w.Output.Write(data)
	w.Remaining -= int64(n)
	return n, err
}

func validToolName(name string, valid *regexp.Regexp) bool {
	const maxSegments = 3
	parts := strings.Split(name, ".")
	if len(parts) > maxSegments {
		return false
	}
	for _, part := range parts {
		if !valid.MatchString(part) || part == "__proto__" || part == "constructor" || part == "prototype" {
			return false
		}
	}
	return true
}
