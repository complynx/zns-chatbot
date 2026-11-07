package render

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

// ErrInvalidEntities identifies unsupported, overlapping, or out-of-bounds entities.
var ErrInvalidEntities = errors.New("invalid Telegram message entities")

// Validate checks text limits, UTF-16 boundaries, and Telegram entity nesting rules.
func Validate(message Message) error {
	if strings.TrimSpace(message.Text) == "" || !utf8.ValidString(message.Text) || unitLen(message.Text) > maxUnits {
		return ErrInvalidEntities
	}
	boundaries := map[int]bool{0: true}
	units := 0
	for _, char := range message.Text {
		units += unitLen(string(char))
		boundaries[units] = true
	}
	for index, entity := range message.Entities {
		if !validEntity(entity, units, boundaries) {
			return ErrInvalidEntities
		}
		for _, previous := range message.Entities[:index] {
			if !validPair(previous, entity) {
				return ErrInvalidEntities
			}
		}
	}
	return nil
}

func validEntity(entity telegram.MessageEntity, units int, boundaries map[int]bool) bool {
	if entity.Offset < 0 || entity.Length <= 0 || entity.Offset > units || entity.Length > units-entity.Offset {
		return false
	}
	if !boundaries[entity.Offset] || !boundaries[entity.Offset+entity.Length] {
		return false
	}
	switch entity.Type {
	case bold, italic, strikethrough, code, pre:
		return true
	case "text_link":
		return entity.URL != ""
	case "text_mention":
		return entity.User != nil && entity.User.ID > 0
	default:
		return false
	}
}

func validPair(a, b telegram.MessageEntity) bool {
	aEnd, bEnd := a.Offset+a.Length, b.Offset+b.Length
	if aEnd <= b.Offset || bEnd <= a.Offset {
		return true
	}
	if a.Type == code || a.Type == pre || b.Type == code || b.Type == pre {
		return false
	}
	contained := (a.Offset <= b.Offset && bEnd <= aEnd) || (b.Offset <= a.Offset && aEnd <= bEnd)
	return contained && (style(a.Type) || style(b.Type))
}

func style(kind string) bool {
	return kind == bold || kind == italic || kind == strikethrough
}
