package telegram

import (
	"context"
	"strings"
)

// Sticker covers static, animated and video stickers, including custom emoji.
type Sticker struct {
	FileID        string     `json:"file_id"`
	UniqueID      string     `json:"file_unique_id"`
	Type          string     `json:"type"`
	Width         int        `json:"width"`
	Height        int        `json:"height"`
	IsAnimated    bool       `json:"is_animated"`
	IsVideo       bool       `json:"is_video"`
	Thumbnail     *MediaFile `json:"thumbnail,omitempty"`
	Emoji         string     `json:"emoji,omitempty"`
	SetName       string     `json:"set_name,omitempty"`
	CustomEmojiID string     `json:"custom_emoji_id,omitempty"`
	SizeBytes     int64      `json:"file_size,omitempty"`
}

type StickerSet struct {
	Name     string    `json:"name"`
	Title    string    `json:"title"`
	Type     string    `json:"sticker_type"`
	Stickers []Sticker `json:"stickers"`
}

func (c *Client) CustomEmojiStickers(ctx context.Context, ids []string) ([]Sticker, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, ErrFileUnavailable
	}
	for _, id := range ids {
		if !numericStickerID(id) {
			return nil, ErrFileUnavailable
		}
	}
	var result []Sticker
	err := c.call(ctx, "getCustomEmojiStickers", struct {
		IDs []string `json:"custom_emoji_ids"`
	}{ids}, &result, false)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	seen := map[string]bool{}
	for _, item := range result {
		if !allowed[item.CustomEmojiID] || seen[item.CustomEmojiID] || item.FileID == "" {
			return nil, ErrFileUnavailable
		}
		seen[item.CustomEmojiID] = true
	}
	return result, nil
}

func (c *Client) StickerSet(ctx context.Context, name string) (StickerSet, error) {
	var result StickerSet
	if len(name) == 0 || len(name) > 64 ||
		strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
		return result, ErrFileUnavailable
	}
	err := c.call(ctx, "getStickerSet", struct {
		Name string `json:"name"`
	}{name}, &result, false)
	if err == nil && (result.Name != name || len(result.Stickers) > 200) {
		return StickerSet{}, ErrFileUnavailable
	}
	return result, err
}

func numericStickerID(id string) bool {
	return id != "" && len(id) <= 32 && strings.Trim(id, "0123456789") == ""
}
