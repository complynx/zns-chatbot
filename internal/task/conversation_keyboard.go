package task

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const replyTokenBytes = 8
const decimalRadix = 10

// PostgreSQL JSONB can reorder argument objects. Preserve numbers exactly while
// canonicalizing object order so a stored presentation keeps the same identity.
func canonicalPrompt(raw []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return raw
	}
	canonical, _ := json.Marshal(normalizePromptNumbers(value))
	return canonical
}

func normalizePromptNumbers(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizePromptNumbers(item)
		}
	case []any:
		for n, item := range typed {
			typed[n] = normalizePromptNumbers(item)
		}
	case json.Number:
		return canonicalNumber(typed)
	}
	return value
}

// Normalize decimal/exponent spelling without rounding or allocating huge powers.
func canonicalNumber(value json.Number) json.Number {
	text := value.String()
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	coefficient, exponent, hasExponent := strings.Cut(strings.ToLower(text), "e")
	whole, fraction, _ := strings.Cut(coefficient, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return json.Number("0")
	}
	significant := strings.TrimRight(digits, "0")
	var power big.Int
	if hasExponent {
		_, _ = power.SetString(exponent, decimalRadix)
	}
	power.Add(&power, big.NewInt(int64(len(digits)-len(significant)-len(fraction))))
	sign := ""
	if negative {
		sign = "-"
	}
	return json.Number(sign + significant + "e" + power.String())
}

func keyboardPromptKey(prompt any) string {
	raw, _ := json.Marshal(prompt)
	sum := sha256.Sum256(canonicalPrompt(raw))
	return hex.EncodeToString(sum[:replyTokenBytes])
}

func questionKeyboard(q loom.Question) *telegram.ReplyKeyboardMarkup {
	keyboard := &telegram.ReplyKeyboardMarkup{ResizeKeyboard: true, OneTimeKeyboard: true}
	for n, choice := range q.Choices {
		if n >= maxChoices {
			break
		}
		keyboard.Keyboard = append(
			keyboard.Keyboard,
			[]telegram.KeyboardButton{{Text: choice.Label}},
		)
	}
	return keyboard
}

func approvalKeyboard(yes, no string) *telegram.ReplyKeyboardMarkup {
	return &telegram.ReplyKeyboardMarkup{
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
		Keyboard:        [][]telegram.KeyboardButton{{{Text: yes}, {Text: no}}},
	}
}

func setReplyMarkup(out []db.Outbound, markup telegram.ReplyMarkup) {
	if len(out) == 0 {
		return
	}
	var payload telegram.SendMessageRequest
	_ = json.Unmarshal(out[len(out)-1].Payload, &payload)
	payload.ReplyMarkup = markup
	out[len(out)-1].Payload, _ = json.Marshal(payload)
}

func removeReplyKeyboard(out []db.Outbound) {
	setReplyMarkup(out, &telegram.ReplyKeyboardRemove{RemoveKeyboard: true})
}

func liveKeyboardID(run *loom.Run) string {
	if run == nil {
		return ""
	}
	if q := run.Question; q != nil && q.Answer == nil && q.ExpiresAt.After(time.Now()) {
		if len(q.Choices) > 0 {
			return "question:dialog-v1:" + keyboardPromptKey(*q)
		}
		return ""
	}
	if a := run.Approval; a != nil && a.ReplyMode == loom.ReplyModeAgent && a.Decision == "" &&
		a.ExpiresAt.After(time.Now()) {
		return "approval:dialog-v1:" + keyboardPromptKey(*a)
	}
	return ""
}

// A resolved answer is acknowledged; an expired unanswered prompt still warns.
func retiredKeyboardText(run loom.Run, prompt string, ru bool) string {
	answered := strings.HasPrefix(prompt, "question:dialog-v1:") && run.Question != nil && run.Question.Answer != nil
	decided := strings.HasPrefix(prompt, "approval:dialog-v1:") && run.Approval != nil && run.Approval.Decision != ""
	if answered || decided {
		return structuredText(ru, "Response received.", "Ответ получен.")
	}
	return structuredText(ru, "This choice is no longer active.", "Этот выбор больше не актуален.")
}

func replyMarkupHash(markup telegram.ReplyMarkup) string {
	if markup == nil {
		return ""
	}
	raw, _ := json.Marshal(markup)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// This outbox metadata is stripped by the typed Telegram sender. It never enters text.
func recordPromptContext(out []db.Outbound, presentation loom.InputPresentation) {
	raw, _ := json.Marshal(presentation)
	for n := range out {
		var payload map[string]json.RawMessage
		_ = json.Unmarshal(out[n].Payload, &payload)
		payload["_loom_context"] = raw
		out[n].Payload, _ = json.Marshal(payload)
	}
}

func approvalPromptKey(a loom.Approval) string {
	raw, _ := json.Marshal(a)
	sum := sha256.Sum256(canonicalPrompt(raw))
	return hex.EncodeToString(sum[:4])
}
