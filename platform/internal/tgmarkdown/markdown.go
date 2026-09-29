// Package tgmarkdown converts bounded CommonMark text to Telegram MarkdownV2.
package tgmarkdown

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

const (
	MaxInputBytes  = 64 << 10
	maxOutputBytes = 4 * MaxInputBytes
	maxDepth       = 64
	maxNodes       = 16384
)

var (
	ErrTooLong     = errors.New("markdown exceeds size or complexity limit")
	ErrInvalidText = errors.New("markdown must be valid UTF-8 without control characters")
	ErrUnsafeURL   = errors.New("markdown link has an unsupported or invalid target")
)

// Convert accepts CommonMark with strikethrough. The caller sets parse_mode to
// MarkdownV2. Telegram message-length limits and splitting belong to the caller.
// It does not fetch links, interpret HTML, or accept pre-escaped MarkdownV2.
func Convert(source string) (string, error) {
	if len(source) > MaxInputBytes {
		return "", ErrTooLong
	}
	if !validText(source) {
		return "", ErrInvalidText
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	document := []byte(normalized)
	parser := goldmark.New(goldmark.WithExtensions(extension.Strikethrough)).Parser()
	tree := parser.Parse(text.NewReader(document))
	if err := checkTree(tree); err != nil {
		return "", err
	}
	renderer := converter{source: document}
	result, err := renderer.blocks(tree, false)
	if err != nil {
		return "", err
	}
	if len(result) > maxOutputBytes {
		return "", ErrTooLong
	}
	return strings.TrimRight(result, "\n"), nil
}

// Escape quotes literal text for use outside MarkdownV2 code and link targets.
func Escape(value string) string {
	return escapeCharacters(value, "_*[]()~`>#+-=|{}.!\\")
}

func escapeCharacters(value, characters string) string {
	var out strings.Builder
	out.Grow(len(value))
	for _, r := range value {
		if strings.ContainsRune(characters, r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

func validText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func checkTree(tree ast.Node) error {
	depth, nodes := 0, 0
	err := ast.Walk(tree, func(_ ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			depth++
			nodes++
			if depth > maxDepth || nodes > maxNodes {
				return ast.WalkStop, ErrTooLong
			}
		} else {
			depth--
		}
		return ast.WalkContinue, nil
	})
	return err
}
