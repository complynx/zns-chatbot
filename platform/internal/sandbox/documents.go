package sandbox

import (
	"bytes"
	"crypto/rand"
	"image"
	_ "image/jpeg" // Decode Telegram-style photo uploads.
	_ "image/png"  // Decode synthetic PNG photo uploads.
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const privateChat = "private"

func (f *Fake) documentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /lab/document", f.labDocument)
	mux.HandleFunc("POST /lab/photo", f.labDocument)
	for _, kind := range []string{"audio", "voice", "video", "video_note"} {
		mux.HandleFunc("POST /lab/"+kind, f.labDocument)
	}
	mux.HandleFunc("GET /lab/files/{id}", f.labFile)
	mux.HandleFunc("GET /file/{bot}/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("bot") != "bot"+f.Token {
			tgError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		f.fileBytes(w, r)
	})
}

func (f *Fake) labDocument(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	user, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	name := r.URL.Query().Get("filename")
	caption := r.URL.Query().Get("caption")
	if _, ok := f.domainOwner(user); !ok || name == "" || len(name) > 255 || strings.ContainsAny(name, "\r\n/\\") {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if !utf8.ValidString(caption) || utf8.RuneCountInString(caption) > 1024 {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, telegram.MaxDocumentBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		api.JSON(w, http.StatusRequestEntityTooLarge, nil)
		return
	}
	var photo *telegram.PhotoSize
	if r.URL.Path == "/lab/photo" {
		photo, err = sandboxPhoto(body)
		if err != nil {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
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
	id := rand.Text()
	message := sandboxFileMessage(user, caption, telegram.Document{
		FileID: id, UniqueID: id, Filename: name, Size: int64(len(body)), MIME: http.DetectContentType(body),
	}, photo)
	if err = sandboxAV(&message, r.URL.Path, r.URL.Query().Get("duration")); err != nil {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	_, err = f.DB.Exec(r.Context(), `INSERT INTO bot.fake_files(id,filename,body) VALUES($1,$2,$3)`, id, name, body)
	if err != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
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

func sandboxFileMessage(user int64, caption string, doc telegram.Document, photo *telegram.PhotoSize) telegram.Message {
	message := telegram.Message{Caption: caption, Chat: telegram.Chat{ID: user, Type: privateChat},
		From: telegram.User{ID: user}, Document: &doc}
	if photo != nil {
		photo.FileID, photo.UniqueID = doc.FileID, doc.UniqueID
		message.Photo = []telegram.PhotoSize{*photo}
		message.Document = nil
	}
	return message
}

func sandboxPhoto(body []byte) (*telegram.PhotoSize, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || (format != "png" && format != "jpeg") || config.Width <= 0 || config.Height <= 0 ||
		int64(config.Width) > 40000000/int64(config.Height) {
		return nil, telegram.ErrInvalidDocument
	}
	if _, _, err = image.Decode(bytes.NewReader(body)); err != nil {
		return nil, telegram.ErrInvalidDocument
	}
	return &telegram.PhotoSize{Width: config.Width, Height: config.Height, Size: int64(len(body))}, nil
}

func (f *Fake) getFile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"file_id"`
	}
	if api.Decode(w, r, &input) != nil || f.DB == nil {
		tgError(w, http.StatusBadRequest, "invalid file")
		return
	}
	var size int64
	if err := f.DB.QueryRow(r.Context(), `SELECT octet_length(body) FROM bot.fake_files WHERE id=$1`, input.ID).
		Scan(&size); err != nil {
		tgError(w, http.StatusBadRequest, "file not found")
		return
	}
	tgOK(w, telegram.File{FileID: input.ID, Path: input.ID, Size: size})
}

func (f *Fake) labFile(w http.ResponseWriter, r *http.Request) {
	user, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	if _, known := f.deliveryDestination(strconv.FormatInt(user, 10)); !known {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	found := false
	for _, message := range f.messages {
		if message.Chat.ID == user &&
			(messageHasFile(message, r.PathValue("id")) || f.messageHasSticker(message, r.PathValue("id"))) {
			found = true
			break
		}
	}
	f.mu.Unlock()
	if !found {
		http.NotFound(w, r)
		return
	}
	f.fileBytes(w, r)
}

func messageHasFile(message telegram.Message, id string) bool {
	if attachment, present, err := telegram.SelectAV(message); present && err == nil &&
		attachment.Document.FileID == id {
		return true
	}
	if message.Document != nil && message.Document.FileID == id {
		return true
	}
	for _, photo := range message.Photo {
		if photo.FileID == id {
			return true
		}
	}
	return false
}

func (f *Fake) fileBytes(w http.ResponseWriter, r *http.Request) {
	if f.DB == nil {
		http.NotFound(w, r)
		return
	}
	var name string
	var body []byte
	if err := f.DB.QueryRow(r.Context(), `SELECT filename,body FROM bot.fake_files WHERE id=$1`, r.PathValue("id")).
		Scan(&name, &body); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if contentType := http.DetectContentType(
		body,
	); contentType == "image/png" || contentType == "image/jpeg" ||
		contentType == webpMIME {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	if r.URL.Query().Get("preview") == "1" {
		switch contentType := http.DetectContentType(body); contentType {
		case webpMIME, "audio/wave", "audio/mpeg", "audio/aiff", "application/ogg", "video/mp4", "video/webm":
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Disposition", "inline")
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}

func (f *Fake) forwardMessage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Chat    int64 `json:"chat_id"`
		From    int64 `json:"from_chat_id"`
		Message int64 `json:"message_id"`
	}
	if api.Decode(w, r, &input) != nil {
		tgError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	chat, known := f.deliveryDestination(strconv.FormatInt(input.Chat, 10))
	if !known {
		tgError(w, http.StatusBadRequest, "chat not found")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var original telegram.Message
	for _, message := range f.messages {
		if message.Chat.ID == input.From && message.ID == input.Message {
			original = message
			break
		}
	}
	if original.ID == 0 {
		tgError(w, http.StatusBadRequest, "message not found")
		return
	}
	f.next++
	original.ID = f.next
	original.Chat = chat
	original.From = telegram.User{ID: fakeBotID, IsBot: true}
	original.Markup = telegram.Markup{}
	f.messages = append(f.messages, original)
	if err := f.save(r.Context()); err != nil {
		tgError(w, http.StatusServiceUnavailable, "state unavailable")
		return
	}
	tgOK(w, original)
}
