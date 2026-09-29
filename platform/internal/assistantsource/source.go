// Package assistantsource refreshes untrusted, source-owned assistant evidence.
package assistantsource

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

var ErrInvalid = errors.New("invalid_source")
var ErrLimit = errors.New("source_limit")
var ErrFetch = errors.New("fetch_failed")

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func readBounded(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, knowledge.MaxSourceBytes+1))
	if err != nil {
		return nil, ErrFetch
	}
	if len(data) > knowledge.MaxSourceBytes {
		return nil, ErrLimit
	}
	if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return nil, ErrInvalid
	}
	return data, nil
}

// ParseQA keeps optional fields and alternates in original entry order.
func ParseQA(data []byte) ([]string, error) {
	if len(data) > knowledge.MaxSourceBytes {
		return nil, ErrLimit
	}
	if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return nil, ErrInvalid
	}
	var document struct {
		Collection []struct {
			Question  *string  `yaml:"question"`
			Alternate []string `yaml:"alt_questions"`
			Answer    *string  `yaml:"answer"`
		} `yaml:"collection"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if decoder.Decode(&document) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) || document.Collection == nil {
		return nil, ErrInvalid
	}
	const maxEntries = 10000
	if len(document.Collection) > maxEntries {
		return nil, ErrLimit
	}
	entries := make([]string, 0, len(document.Collection))
	total := 0
	for _, entry := range document.Collection {
		lines := []string{}
		if entry.Question != nil {
			lines = append(lines, "Q: "+*entry.Question)
		}
		if entry.Alternate != nil {
			lines = append(lines, " / "+strings.Join(entry.Alternate, " / "))
		}
		if entry.Answer != nil {
			lines = append(lines, "A: "+*entry.Answer)
		}
		text := strings.Join(lines, "\n") + "\n"
		total += len(text)
		if total > knowledge.MaxSourceBytes {
			return nil, ErrLimit
		}
		entries = append(entries, text)
	}
	return entries, nil
}

var codeSpans = regexp.MustCompile("`+[^`]*`+")
var escapedMarkdown = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+.!~=>-])")

// UnescapeMarkdown matches the legacy outside-backtick transformation.
func UnescapeMarkdown(text string) string {
	var result strings.Builder
	offset := 0
	for _, span := range codeSpans.FindAllStringIndex(text, -1) {
		result.WriteString(escapedMarkdown.ReplaceAllString(text[offset:span[0]], "$1"))
		result.WriteString(text[span[0]:span[1]])
		offset = span[1]
	}
	result.WriteString(escapedMarkdown.ReplaceAllString(text[offset:], "$1"))
	return result.String()
}
