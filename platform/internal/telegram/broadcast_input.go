package telegram

import (
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
)

const closingAnchor = "</a>"

var ErrEntityRange = errors.New("invalid Telegram entity range")

// CommandText quotes CODE/PRE arguments before shell-style argument parsing.
func CommandText(message Message) (string, error) {
	boundaries := utf16Boundaries(message.Text)
	entities := append([]MessageEntity(nil), message.Entities...)
	sort.SliceStable(entities, func(i, j int) bool { return entities[i].Offset < entities[j].Offset })
	var output strings.Builder
	previous := 0
	for _, entity := range entities {
		if entity.Type != "code" && entity.Type != "pre" {
			continue
		}
		start, end, err := entityBytes(boundaries, entity)
		if err != nil || start < previous {
			return "", ErrEntityRange
		}
		output.WriteString(message.Text[previous:start])
		output.WriteByte('\'')
		output.WriteString(strings.ReplaceAll(message.Text[start:end], "'", "'\\''"))
		output.WriteByte('\'')
		previous = end
	}
	output.WriteString(message.Text[previous:])
	return output.String(), nil
}

func entityBytes(boundaries []int, entity MessageEntity) (int, int, error) {
	if entity.Offset < 0 || entity.Length <= 0 || entity.Offset >= len(boundaries) ||
		entity.Length > len(boundaries)-1-entity.Offset {
		return 0, 0, ErrEntityRange
	}
	start, end := boundaries[entity.Offset], boundaries[entity.Offset+entity.Length]
	if start < 0 || end < 0 {
		return 0, 0, ErrEntityRange
	}
	return start, end, nil
}

type htmlEntity struct {
	start, end  int
	open, close string
}

// MessageHTML serializes authoritative message text/entities for broadcast input.
// Overlapping non-nested entities fail instead of changing the visible text.
func MessageHTML(message Message) (string, error) {
	boundaries := utf16Boundaries(message.Text)
	var spans []htmlEntity
	for _, entity := range message.Entities {
		start, end, err := entityBytes(boundaries, entity)
		if err != nil {
			return "", err
		}
		open, closing := entityTags(entity, message.Text[start:end])
		if open != "" {
			spans = append(spans, htmlEntity{start, end, open, closing})
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end > spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	var output strings.Builder
	var stack []htmlEntity
	position := 0
	for _, span := range spans {
		for len(stack) > 0 && stack[len(stack)-1].end <= span.start {
			last := stack[len(stack)-1]
			output.WriteString(html.EscapeString(message.Text[position:last.end]))
			output.WriteString(last.close)
			position = last.end
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && span.end > stack[len(stack)-1].end {
			return "", ErrEntityRange
		}
		output.WriteString(html.EscapeString(message.Text[position:span.start]))
		output.WriteString(span.open)
		position = span.start
		stack = append(stack, span)
	}
	for len(stack) > 0 {
		last := stack[len(stack)-1]
		output.WriteString(html.EscapeString(message.Text[position:last.end]))
		output.WriteString(last.close)
		position = last.end
		stack = stack[:len(stack)-1]
	}
	output.WriteString(html.EscapeString(message.Text[position:]))
	return output.String(), nil
}

func entityTags(entity MessageEntity, text string) (string, string) {
	switch entity.Type {
	case "bold":
		return "<b>", "</b>"
	case "italic":
		return "<i>", "</i>"
	case "underline":
		return "<u>", "</u>"
	case "strikethrough":
		return "<s>", "</s>"
	case "spoiler":
		return "<tg-spoiler>", "</tg-spoiler>"
	case "blockquote":
		return "<blockquote>", "</blockquote>"
	case "expandable_blockquote":
		return "<blockquote expandable>", "</blockquote>"
	case "code":
		return "<code>", "</code>"
	case "pre":
		if entity.Language != "" {
			return `<pre><code class="language-` + html.EscapeString(entity.Language) + `">`, "</code></pre>"
		}
		return "<pre>", "</pre>"
	case "text_link":
		return `<a href="` + html.EscapeString(entity.URL) + `">`, closingAnchor
	case "text_mention":
		if entity.User != nil {
			return fmt.Sprintf(`<a href="tg://user?id=%d">`, entity.User.ID), closingAnchor
		}
	case "custom_emoji":
		return `<tg-emoji emoji-id="` + html.EscapeString(entity.CustomEmojiID) + `">`, "</tg-emoji>"
	case "url":
		return `<a href="` + html.EscapeString(text) + `">`, closingAnchor
	}
	return "", ""
}
