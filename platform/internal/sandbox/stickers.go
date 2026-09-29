package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const sandboxStickerLimit = 1 << 20
const webpMIME = "image/webp"
const sandboxStickerSide = 512

func (f *Fake) stickerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /lab/sticker", f.labSticker)
	mux.HandleFunc("POST /lab/custom_emoji", f.labSticker)
}

func (f *Fake) labSticker(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	query := r.URL.Query()
	user, _ := strconv.ParseInt(query.Get("user"), 10, 64)
	name, caption := query.Get("filename"), query.Get("caption")
	if _, ok := identity.Subject(user); !ok || name == "" || len(name) > 255 || strings.ContainsAny(name, "\r\n/\\") ||
		!utf8.ValidString(caption) || utf8.RuneCountInString(caption) > 1024 {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, sandboxStickerLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		api.JSON(w, http.StatusRequestEntityTooLarge, nil)
		return
	}
	sticker, err := sandboxSticker(body, query)
	if err != nil {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	message := telegram.Message{
		Chat:    telegram.Chat{ID: user, Type: privateChat},
		From:    telegram.User{ID: user},
		Sticker: &sticker,
	}
	if r.URL.Path == "/lab/custom_emoji" {
		sticker.Type = "custom_emoji"
		sticker.CustomEmojiID = sandboxEmojiID(body)
		message.Sticker = nil
		message.Text, message.Entities, err = sandboxEmojiText(caption, query.Get("marker"), sticker.CustomEmojiID)
	} else if caption != "" {
		err = telegram.ErrCustomEmojiEntity // Telegram stickers do not have captions.
	}
	if err != nil {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if f.DB == nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) >= maxPendingUpdates {
		api.JSON(w, http.StatusTooManyRequests, nil)
		return
	}
	_, err = f.DB.Exec(
		r.Context(),
		`INSERT INTO bot.fake_files(id,filename,body) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`,
		sticker.FileID,
		name,
		body,
	)
	if err != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	if sticker.CustomEmojiID != "" {
		f.rememberSticker(sticker)
	}
	f.next++
	message.ID = f.next
	update := telegram.Update{ID: f.next, Message: &message}
	f.messages = append(f.messages, message)
	f.updates = append(f.updates, update)
	if err = f.save(r.Context()); err != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	api.JSON(w, http.StatusOK, update)
}

func (f *Fake) rememberSticker(sticker telegram.Sticker) {
	if f.stickers == nil {
		f.stickers = make(map[string]telegram.Sticker)
	}
	f.stickers[sticker.CustomEmojiID] = sticker
}

func sandboxEmojiID(body []byte) string {
	sum := sha256.Sum256(body)
	return new(big.Int).SetBytes(sum[:]).String()
}

// Only format admission happens here. Actual artwork decoding belongs to the media worker.
func sandboxSticker(body []byte, query url.Values) (telegram.Sticker, error) {
	sum := sha256.Sum256(body)
	id := "sticker-" + hex.EncodeToString(sum[:])
	sticker := telegram.Sticker{
		FileID:   id,
		UniqueID: id,
		Type:     "regular",
		Width:    sandboxStickerSide,
		Height:   sandboxStickerSide,
		Size:     int64(len(body)),
	}
	switch strings.ToLower(filepath.Ext(query.Get("filename"))) {
	case ".webp":
		if http.DetectContentType(body) != webpMIME {
			return sticker, telegram.ErrInvalidDocument
		}
	case ".webm":
		if http.DetectContentType(body) != "video/webm" {
			return sticker, telegram.ErrInvalidDocument
		}
		sticker.IsVideo = true
	case ".tgs":
		if len(body) < 3 || body[0] != 0x1f || body[1] != 0x8b || body[2] != 8 {
			return sticker, telegram.ErrInvalidDocument
		}
		sticker.IsAnimated = true
	default:
		return sticker, telegram.ErrInvalidDocument
	}
	for key, expected := range map[string]bool{"is_animated": sticker.IsAnimated, "is_video": sticker.IsVideo} {
		if query.Has(key) && query.Get(key) != strconv.FormatBool(expected) {
			return sticker, telegram.ErrInvalidDocument
		}
	}
	return sticker, nil
}

// Every marker occurrence becomes an entity; UTF-16 counts match Telegram's offsets.
func sandboxEmojiText(text, marker, id string) (string, []telegram.MessageEntity, error) {
	if marker == "" {
		marker = "🧩"
	}
	if !utf8.ValidString(marker) || utf8.RuneCountInString(marker) > 16 {
		return "", nil, telegram.ErrCustomEmojiEntity
	}
	if text == "" {
		text = marker
	}
	entities := []telegram.MessageEntity{}
	for position := 0; position < len(text); {
		relative := strings.Index(text[position:], marker)
		if relative < 0 {
			break
		}
		position += relative
		entities = append(
			entities,
			telegram.MessageEntity{
				Type:          "custom_emoji",
				Offset:        len(utf16.Encode([]rune(text[:position]))),
				Length:        len(utf16.Encode([]rune(marker))),
				CustomEmojiID: id,
			},
		)
		position += len(marker)
	}
	if len(entities) == 0 {
		return "", nil, telegram.ErrCustomEmojiEntity
	}
	_, err := telegram.CustomEmojiSpans(text, entities)
	return text, entities, err
}

func (f *Fake) getCustomEmojiStickers(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IDs []string `json:"custom_emoji_ids"`
	}
	if api.Decode(w, r, &input) != nil || len(input.IDs) > telegram.MaxCustomEmoji {
		tgError(w, http.StatusBadRequest, "invalid emoji ids")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stickers := []telegram.Sticker{}
	for _, id := range input.IDs {
		sticker, ok := f.stickers[id]
		if !ok {
			tgError(w, http.StatusBadRequest, "emoji not found")
			return
		}
		stickers = append(stickers, sticker)
	}
	tgOK(w, stickers)
}

func (f *Fake) ownerStickers(messages []telegram.Message) map[string]telegram.Sticker {
	stickers := make(map[string]telegram.Sticker)
	for _, message := range messages {
		for _, entity := range append(append([]telegram.MessageEntity{}, message.Entities...), message.CaptionEntities...) {
			if sticker, ok := f.stickers[entity.CustomEmojiID]; ok {
				stickers[entity.CustomEmojiID] = sticker
			}
		}
	}
	return stickers
}

func (f *Fake) messageHasSticker(message telegram.Message, id string) bool {
	if message.Sticker != nil && message.Sticker.FileID == id {
		return true
	}
	for _, sticker := range f.ownerStickers([]telegram.Message{message}) {
		if sticker.FileID == id {
			return true
		}
	}
	return false
}
