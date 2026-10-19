package telegram

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

const maxDocumentBytes = 4 * 1024 * 1024
const maxDocumentNameLength = 256

type SendDocumentRequest struct {
	ChatID   int64
	Filename string
	Content  []byte
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
	err = c.do(req, &message, true)
	if err == nil && (message.ID <= 0 || message.Chat.ID != input.ChatID) {
		err = &Error{Outcome: Unknown}
	}
	return Receipt{MessageID: message.ID, ChatID: message.Chat.ID}, err
}
