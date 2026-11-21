package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const maxDocumentBytes = 4 * 1024 * 1024
const maxDocumentNameLength = 256

type SendDocumentRequest struct {
	ChatID      int64
	Filename    string
	Content     []byte
	ReplyMarkup *InlineKeyboardMarkup
}

// SendDocument uploads bounded local bytes. URLs and Telegram file IDs are not accepted.
func (c *Client) SendDocument(ctx context.Context, input SendDocumentRequest) (Receipt, error) {
	if ctx.Err() != nil || input.ChatID <= 0 || len(input.Content) == 0 || len(input.Content) > maxDocumentBytes ||
		input.Filename == "" || len(input.Filename) > maxDocumentNameLength ||
		strings.ContainsAny(input.Filename, `/\`) || strings.IndexFunc(input.Filename, unicode.IsControl) >= 0 {
		return Receipt{}, &Error{Outcome: NotStarted}
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("chat_id", strconv.FormatInt(input.ChatID, 10)); err != nil {
		return Receipt{}, &Error{Outcome: NotStarted}
	}
	if input.ReplyMarkup != nil {
		raw, err := json.Marshal(input.ReplyMarkup)
		if err != nil {
			return Receipt{}, &Error{Outcome: NotStarted}
		}
		if err = form.WriteField("reply_markup", string(raw)); err != nil {
			return Receipt{}, &Error{Outcome: NotStarted}
		}
	}
	file, err := form.CreateFormFile("document", input.Filename)
	if err != nil {
		return Receipt{}, &Error{Outcome: NotStarted}
	}
	if _, err = file.Write(input.Content); err != nil {
		return Receipt{}, &Error{Outcome: NotStarted}
	}
	if err = form.Close(); err != nil {
		return Receipt{}, &Error{Outcome: NotStarted}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/sendDocument", &body)
	if err != nil {
		return Receipt{}, &Error{Outcome: NotStarted}
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	var message Message
	err = doRequest(c.http, req, &message, true)
	if err == nil && (message.ID <= 0 || message.Chat.ID != input.ChatID) {
		err = &Error{Outcome: Unknown}
	}
	return Receipt{MessageID: message.ID, ChatID: message.Chat.ID}, err
}

const MaxArchiveBytes = 50 << 20
const archiveUploadTimeout = 5 * time.Minute

// SendArchive streams a server-generated ZIP into the owner's private chat.
// The caller receives only the delivery outcome.
func (c *Client) SendArchive(ctx context.Context, owner int64, filename string, archive io.Reader, size int64) error {
	if owner <= 0 || archive == nil || size <= 0 || size > MaxArchiveBytes || filename == "" ||
		strings.ContainsAny(filename, `/\`) || strings.IndexFunc(filename, unicode.IsControl) >= 0 {
		return &Error{Outcome: NotStarted}
	}
	// Keep the multipart headers and final boundary in memory, separately from the archive.
	var frame bytes.Buffer
	form := multipart.NewWriter(&frame)
	if err := form.WriteField("chat_id", strconv.FormatInt(owner, 10)); err != nil {
		return &Error{Outcome: NotStarted}
	}
	if _, err := form.CreateFormFile("document", filename); err != nil {
		return &Error{Outcome: NotStarted}
	}
	start := frame.Len()
	if err := form.Close(); err != nil {
		return &Error{Outcome: NotStarted}
	}
	parts := frame.Bytes()
	body := io.MultiReader(bytes.NewReader(parts[:start]), io.LimitReader(archive, size), bytes.NewReader(parts[start:]))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/sendDocument", body)
	if err != nil {
		return &Error{Outcome: NotStarted}
	}
	req.ContentLength = int64(len(parts)) + size
	req.Header.Set("Content-Type", form.FormDataContentType())
	var message Message
	// Bulk uploads share the transport and redirect policy but need more time than a bot RPC.
	upload := &http.Client{
		Transport:     c.http.Transport,
		Jar:           c.http.Jar,
		CheckRedirect: c.http.CheckRedirect,
		Timeout:       archiveUploadTimeout,
	}
	if err = doRequest(upload, req, &message, true); err != nil {
		return err
	}
	if message.ID <= 0 || message.Chat.ID != owner {
		return &Error{Outcome: Unknown}
	}
	return nil
}
