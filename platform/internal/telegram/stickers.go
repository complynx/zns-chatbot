package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const MaxCustomEmoji = 200

var (
	ErrCustomEmojiLimit  = errors.New("custom emoji count exceeds 200")
	ErrCustomEmojiEntity = errors.New("invalid custom emoji entity")
	ErrCustomEmojiID     = errors.New("invalid custom emoji identifier")
)

type MessageEntity struct {
	URL           string `json:"url,omitempty"`
	User          *User  `json:"user,omitempty"`
	Language      string `json:"language,omitempty"`
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}

// Sticker keeps Telegram's actual media identity and animation flags; Emoji is only alternative text.
type Sticker struct {
	FileID          string     `json:"file_id"`
	UniqueID        string     `json:"file_unique_id"`
	Type            string     `json:"type"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	IsAnimated      bool       `json:"is_animated"`
	IsVideo         bool       `json:"is_video"`
	Emoji           string     `json:"emoji,omitempty"`
	CustomEmojiID   string     `json:"custom_emoji_id,omitempty"`
	Size            int64      `json:"file_size,omitempty"`
	Thumbnail       *PhotoSize `json:"thumbnail,omitempty"`
	NeedsRepainting bool       `json:"needs_repainting,omitempty"`
}

// GetCustomEmojiStickers returns media metadata. Match responses by CustomEmojiID, not array position.
func (c Client) GetCustomEmojiStickers(ctx context.Context, ids []string) ([]Sticker, error) {
	if len(ids) > MaxCustomEmoji {
		return nil, ErrCustomEmojiLimit
	}
	if len(ids) == 0 {
		return nil, nil
	}
	for _, id := range ids {
		if !validCustomEmojiID(id) {
			return nil, ErrCustomEmojiID
		}
	}
	var stickers []Sticker
	input := struct {
		IDs []string `json:"custom_emoji_ids"`
	}{IDs: ids}
	if err := c.Call(ctx, "getCustomEmojiStickers", input, &stickers); err != nil {
		return nil, fmt.Errorf("get custom emoji stickers: %w", err)
	}
	return stickers, nil
}

// CustomEmojiSpan preserves Telegram UTF-16 positions and the original placeholder text.
// The placeholder is not a description of the custom image or animation.
type CustomEmojiSpan struct {
	ID     string
	Offset int
	Length int
	Text   string
}

// CustomEmojiSpans accepts either text/entities or caption/caption_entities. It preserves
// entity order and repeated IDs. Only overlapping custom emoji entities are rejected;
// ordinary formatting may legitimately contain an emoji entity.
func CustomEmojiSpans(text string, entities []MessageEntity) ([]CustomEmojiSpan, error) {
	const maxEntities = 1000
	if len(entities) > maxEntities || !utf8.ValidString(text) {
		return nil, ErrCustomEmojiEntity
	}
	boundaries := utf16Boundaries(text)
	spans := make([]CustomEmojiSpan, 0)
	for _, entity := range entities {
		if entity.Type != "custom_emoji" {
			continue
		}
		if len(spans) == MaxCustomEmoji {
			return nil, ErrCustomEmojiLimit
		}
		if !validCustomEmojiID(entity.CustomEmojiID) || entity.Offset < 0 || entity.Length <= 0 ||
			entity.Offset >= len(boundaries) || entity.Length > len(boundaries)-1-entity.Offset {
			return nil, ErrCustomEmojiEntity
		}
		end := entity.Offset + entity.Length
		if boundaries[entity.Offset] < 0 || boundaries[end] < 0 {
			return nil, ErrCustomEmojiEntity
		}
		for _, span := range spans {
			if entity.Offset < span.Offset+span.Length && span.Offset < end {
				return nil, ErrCustomEmojiEntity
			}
		}
		spans = append(spans, CustomEmojiSpan{ID: entity.CustomEmojiID, Offset: entity.Offset,
			Length: entity.Length, Text: text[boundaries[entity.Offset]:boundaries[end]]})
	}
	return spans, nil
}

func validCustomEmojiID(id string) bool {
	const maxIDBytes = 256
	return id != "" && len(id) <= maxIDBytes && strings.TrimSpace(id) == id
}

func utf16Boundaries(text string) []int {
	const surrogatePairUnits = 2
	boundaries := make([]int, 0, len(text)+1)
	for offset, value := range text {
		boundaries = append(boundaries, offset)
		if utf16.RuneLen(value) == surrogatePairUnits {
			boundaries = append(boundaries, -1)
		}
	}
	return append(boundaries, len(text))
}
