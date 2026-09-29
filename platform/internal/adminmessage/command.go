package adminmessage

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
)

func (s Service) commandRecipients(ctx context.Context, actor, raw string) ([]Destination, error) {
	if strings.HasPrefix(raw, "$") {
		return s.ResolveShortcut(ctx, actor, raw)
	}
	if strings.HasPrefix(raw, "[") {
		var values []json.RawMessage
		if json.Unmarshal([]byte(raw), &values) != nil {
			return nil, invalid()
		}
		result := make([]Destination, 0, len(values))
		for _, value := range values {
			text := string(value)
			if strings.HasPrefix(text, `"`) && json.Unmarshal(value, &text) != nil {
				return nil, invalid()
			}
			destination, err := literalDestination(text)
			if err != nil {
				return nil, err
			}
			result = append(result, destination)
		}
		return result, nil
	}
	destination, err := literalDestination(raw)
	return []Destination{destination}, err
}

func literalDestination(raw string) (Destination, error) {
	chat, thread, found := strings.Cut(raw, ":")
	destination := Destination{Chat: chat}
	if found {
		value, err := strconv.ParseInt(thread, 10, 64)
		if err != nil || value <= 0 {
			return Destination{}, invalid()
		}
		destination.Thread = value
	}
	return destination, nil
}

// commandWords supports shell-style quoted strings without variable expansion.
func commandWords(raw string) ([]string, error) {
	var result []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, char := range raw {
		switch {
		case escaped:
			word.WriteRune(char)
			escaped = false
		case char == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
			started = true
		case unicode.IsSpace(char):
			if started {
				result = append(result, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(char)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, invalid()
	}
	if started {
		result = append(result, word.String())
	}
	return result, nil
}
