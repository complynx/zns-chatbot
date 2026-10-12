// Package render converts agent Markdown to Telegram text and UTF-16 entities.
package render

import (
	"cmp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"

	"github.com/complynx/shuttle/internal/telegram"
)

const (
	maxUnits      = 4096
	bold          = "bold"
	italic        = "italic"
	strikethrough = "strikethrough"
	code          = "code"
	pre           = "pre"
)

// Message is a sendable chunk. Text is also the fallback after a formatting rejection.
type Message struct {
	Text     string
	Entities []telegram.MessageEntity
}

type renderer struct {
	source   []byte
	mentions map[int64]telegram.User
	output   strings.Builder
	units    int
	entities []telegram.MessageEntity
}

// Render accepts imperfect Markdown and mentions only users verified by the caller.
func Render(markdown string, mentions map[int64]telegram.User) []Message {
	source := []byte(strings.ToValidUTF8(markdown, "�"))
	document := parser.New(parser.WithExtensions(extension.StrikethroughParser)).Parse(source)
	r := renderer{source: source, mentions: mentions}
	r.children(document)
	slices.SortFunc(r.entities, func(a, b telegram.MessageEntity) int {
		if order := cmp.Compare(a.Offset, b.Offset); order != 0 {
			return order
		}
		return cmp.Compare(b.Length, a.Length)
	})
	return split(r.output.String(), r.entities)
}

// Plain splits text without interpreting Markdown or adding entities.
func Plain(value string) []Message {
	return split(strings.ToValidUTF8(value, "�"), nil)
}

func (r *renderer) write(value string) {
	r.output.WriteString(value)
	r.units += unitLen(value)
}

func (r *renderer) children(node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.node(child)
		if _, block := child.(ast.BlockNode); block && child.NextSibling() != nil {
			r.write("\n")
		}
	}
}

func (r *renderer) node(node ast.Node) {
	switch value := node.(type) {
	case *ast.Text:
		r.write(value.Value.Value(r.source))
		if value.SoftLineBreak() || value.HardLineBreak() {
			r.write("\n")
		}
	case *ast.Emphasis:
		r.wrap(node, telegram.MessageEntity{Type: italic})
	case *ast.Strong:
		r.wrap(node, telegram.MessageEntity{Type: bold})
	case *extast.Strikethrough:
		r.wrap(node, telegram.MessageEntity{Type: strikethrough})
	case *ast.Heading:
		r.wrap(node, telegram.MessageEntity{Type: bold})
	case *ast.CodeSpan:
		r.literal(value.Value.Value(r.source), telegram.MessageEntity{Type: code})
	case *ast.CodeBlock:
		language, _ := value.Language(r.source)
		r.literal(value.Value.Str(r.source), telegram.MessageEntity{Type: pre, Language: language})
	case *ast.Link:
		r.wrap(node, r.link(value.Destination.Value(r.source)))
	case *ast.AutoLink:
		r.literal(value.Label.Value(r.source), r.link(value.Destination.Value(r.source)))
	case *ast.RawHTML:
		r.write(value.Value.Value(r.source))
	case *ast.HTMLBlock:
		r.write(value.Value.Str(r.source))
	case *ast.Blockquote:
		r.write("> ")
		r.children(node)
	case *ast.List:
		r.list(value)
	case *ast.ThematicBreak:
		r.write("---")
	default:
		r.children(node)
	}
}

func (r *renderer) list(list *ast.List) {
	index := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		prefix := "- "
		if list.IsOrdered() {
			prefix = strconv.Itoa(index) + ". "
		}
		r.write(prefix)
		r.children(item)
		if item.NextSibling() != nil {
			r.write("\n")
		}
		index++
	}
}

func (r *renderer) literal(value string, entity telegram.MessageEntity) {
	entity.Offset = r.units
	r.write(value)
	r.add(entity, r.units)
}

func (r *renderer) wrap(node ast.Node, entity telegram.MessageEntity) {
	entity.Offset = r.units
	first := len(r.entities)
	r.children(node)
	end := r.units
	// Telegram code/pre cannot overlap any other entity. Preserve surrounding formatting.
	for _, child := range r.entities[first:] {
		if child.Type == code || child.Type == pre {
			r.add(entity, child.Offset)
			entity.Offset = child.Offset + child.Length
		}
	}
	r.add(entity, end)
}

func (r *renderer) add(entity telegram.MessageEntity, end int) {
	if entity.Type != "" && end > entity.Offset {
		entity.Length = end - entity.Offset
		r.entities = append(r.entities, entity)
	}
}

func (r *renderer) link(destination string) telegram.MessageEntity {
	parsed, err := url.Parse(destination)
	if err != nil {
		return telegram.MessageEntity{}
	}
	if parsed.Scheme == "tg" && parsed.Host == "user" {
		id, parseErr := strconv.ParseInt(parsed.Query().Get("id"), 10, 64)
		user, found := r.mentions[id]
		if parseErr == nil && found && id > 0 && user.ID == id {
			return telegram.MessageEntity{Type: "text_mention", User: &user}
		}
		return telegram.MessageEntity{}
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" || parsed.Scheme == "mailto" {
		return telegram.MessageEntity{Type: "text_link", URL: destination}
	}
	return telegram.MessageEntity{}
}

func unitLen(value string) int {
	units := 0
	for _, char := range value {
		units += utf16.RuneLen(char)
	}
	return units
}

func split(value string, entities []telegram.MessageEntity) []Message {
	var messages []Message
	startByte, startUnit := 0, 0
	for startByte < len(value) {
		endByte, endUnit := startByte, startUnit
		for endByte < len(value) {
			char, size := utf8.DecodeRuneInString(value[endByte:])
			units := utf16.RuneLen(char)
			if endUnit-startUnit+units > maxUnits {
				break
			}
			endByte += size
			endUnit += units
		}
		if strings.TrimSpace(value[startByte:endByte]) == "" {
			next := strings.IndexFunc(value[endByte:], func(char rune) bool { return !unicode.IsSpace(char) })
			if next < 0 {
				break
			}
			keptStart := paddingStart(value, startByte, endByte+next)
			startUnit += unitLen(value[startByte:keptStart])
			startByte = keptStart
			continue
		}
		message := Message{Text: value[startByte:endByte]}
		for _, entity := range entities {
			start, end := max(startUnit, entity.Offset), min(endUnit, entity.Offset+entity.Length)
			if end > start {
				entity.Offset, entity.Length = start-startUnit, end-start
				message.Entities = append(message.Entities, entity)
			}
		}
		if Validate(message) != nil {
			message.Entities = nil
		}
		messages = append(messages, message)
		startByte, startUnit = endByte, endUnit
	}
	return messages
}

// Keep as much indentation as fits with the next visible character. Telegram rejects blank chunks.
func paddingStart(value string, start, content int) int {
	char, _ := utf8.DecodeRuneInString(value[content:])
	units := utf16.RuneLen(char)
	for content > start {
		previous, size := utf8.DecodeLastRuneInString(value[start:content])
		if units+utf16.RuneLen(previous) > maxUnits {
			break
		}
		content -= size
		units += utf16.RuneLen(previous)
	}
	return content
}
