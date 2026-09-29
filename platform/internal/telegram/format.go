package telegram

import (
	"errors"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/tgmarkdown"
)

const MarkdownV2 = "MarkdownV2"
const MaxTextUnits = 4096

var ErrMessageText = errors.New("message must contain 1-4096 UTF-16 units of valid text")

// FormatSend converts only explicitly marked model text. Manual prefix/suffix
// strings remain literal. Conversion failures preserve the entire source as plain text.
// Call before computing view hashes; this clears the internal source markers.
func FormatSend(p Send) Send {
	if !p.NativeMarkdown {
		return p
	}
	literal := p.LiteralPrefix + p.Text + p.LiteralSuffix
	converted, err := tgmarkdown.Convert(p.Text)
	if err == nil {
		converted = tgmarkdown.Escape(p.LiteralPrefix) + converted + tgmarkdown.Escape(p.LiteralSuffix)
		_, err = tgmarkdown.ParseV2(converted)
	}
	p.Text, p.ParseMode = literal, ""
	if err == nil {
		p.Text, p.ParseMode = converted, MarkdownV2
	}
	p.NativeMarkdown = false
	p.LiteralPrefix, p.LiteralSuffix = "", ""
	return p
}

func PrepareSend(p Send) (Send, error) {
	p = FormatSend(p)
	if _, _, err := VisibleText(p); err != nil {
		return Send{}, err
	}
	return p, nil
}

// VisibleText validates wire text and decodes the supported MarkdownV2 subset.
// The fake uses the same contract to persist Telegram-shaped visible messages.
func VisibleText(p Send) (string, []MessageEntity, error) {
	text := p.Text
	var entities []MessageEntity
	switch p.ParseMode {
	case "":
	case MarkdownV2:
		parsed, err := tgmarkdown.ParseV2(p.Text)
		if err != nil {
			return "", nil, err
		}
		text = parsed.Text
		for _, entity := range parsed.Entities {
			item := MessageEntity{
				Type:     entity.Type,
				Offset:   entity.Offset,
				Length:   entity.Length,
				URL:      entity.URL,
				Language: entity.Language,
			}
			if entity.UserID != 0 {
				item.User = &User{ID: entity.UserID}
			}
			entities = append(entities, item)
		}
	default:
		return "", nil, tgmarkdown.ErrMarkdownV2
	}
	units := len(utf16.Encode([]rune(text)))
	if !utf8.ValidString(text) || units == 0 || units > MaxTextUnits {
		return "", nil, ErrMessageText
	}
	return text, entities, nil
}
