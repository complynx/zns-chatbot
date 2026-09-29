package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxScriptRuns = 2
const MaxScriptCodeBytes = 4 * 1024

// MaxScriptInputBytes holds a 16k-rune document, including escaped JSON and its envelope.
const MaxScriptInputBytes = 128 * 1024
const MaxScriptResultBytes = 4 * 1024
const planScriptField = "script_action"

// ScriptProposal supplies only explicit data. Identity, API clients and ambient
// application context are never injected into the worker input.
type ScriptProposal struct {
	Code      string `json:"code"`
	InputJSON string `json:"input_json"`
}

type ScriptContext struct {
	UpdateID  int64       `json:"-"`
	Available bool        `json:"available"`
	Remaining int         `json:"remaining"`
	Runs      []ScriptRun `json:"runs"`
}

// ScriptRun carries bounded outcomes; terminal source/input copies are discarded.
type ScriptRun struct {
	Calls  []ScriptToolResult `json:"calls,omitempty"`
	Code   string             `json:"code"`
	Result json.RawMessage    `json:"result,omitempty"`
	Error  string             `json:"error,omitempty"`
}

// ScriptToolResult is host evidence, separate from untrusted JavaScript output.
type ScriptToolResult struct {
	Name   string          `json:"name"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func ValidateScript(p ScriptProposal) error {
	code, _ := json.Marshal(p.Code)
	if strings.TrimSpace(p.Code) == "" || len(code) > MaxScriptCodeBytes || !utf8.ValidString(p.Code) ||
		len(
			p.InputJSON,
		) == 0 || len(p.InputJSON) > MaxScriptInputBytes || !utf8.ValidString(p.InputJSON) || !json.Valid([]byte(p.InputJSON)) {
		return errors.New("invalid script request")
	}
	return nil
}

func validateScriptPlan(p Plan) error {
	if p.ScriptAction == nil {
		return nil
	}
	if p.Action != nil || p.OrderAction != nil || p.ProfileAction != nil || p.MediaAction != nil ||
		p.KnowledgeAction != nil {
		return errors.New("script request cannot include a business action")
	}
	return ValidateScript(*p.ScriptAction)
}
