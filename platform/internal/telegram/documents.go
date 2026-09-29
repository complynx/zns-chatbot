package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const MaxDocumentBytes = 20 << 20

var ErrInvalidDocument = errors.New("document is unavailable or exceeds 20 MiB")

type Document struct {
	FileID   string `json:"file_id"`
	UniqueID string `json:"file_unique_id"`
	Filename string `json:"file_name"`
	MIME     string `json:"mime_type,omitempty"`
	Size     int64  `json:"file_size,omitempty"`
}

// SendDocument uploads immutable stored bytes instead of a mutable source message.
func (c Client) SendDocument(ctx context.Context, chat int64, filename string, body []byte) (Message, error) {
	if len(body) == 0 || len(body) > MaxDocumentBytes {
		return Message{}, ErrInvalidDocument
	}
	var encoded bytes.Buffer
	writer := multipart.NewWriter(&encoded)
	if err := writer.WriteField("chat_id", strconv.FormatInt(chat, 10)); err != nil {
		return Message{}, err
	}
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return Message{}, err
	}
	if _, err = part.Write(body); err != nil {
		return Message{}, err
	}
	if err = writer.Close(); err != nil {
		return Message{}, err
	}
	var message Message
	err = c.request(ctx, "sendDocument", &encoded, writer.FormDataContentType(), &message)
	return message, err
}

type File struct {
	FileID string `json:"file_id"`
	Path   string `json:"file_path"`
	Size   int64  `json:"file_size"`
}

// Download resolves an opaque Telegram file ID; the returned path stays on the Bot API host.
func (c Client) Download(ctx context.Context, document Document) ([]byte, error) {
	if document.FileID == "" || document.Size > MaxDocumentBytes {
		return nil, ErrInvalidDocument
	}
	var file File
	if err := c.Call(ctx, "getFile", map[string]string{"file_id": document.FileID}, &file); err != nil {
		return nil, err
	}
	return c.DownloadResolved(ctx, file)
}

// DownloadResolved reuses trusted getFile metadata without another control call.
// Path, declared size, redirects and actual body size remain bounded here.
func (c Client) DownloadResolved(ctx context.Context, file File) ([]byte, error) {
	if file.Path == "" || file.Size > MaxDocumentBytes {
		return nil, ErrInvalidDocument
	}
	path := strings.Split(file.Path, "/")
	for i, part := range path {
		if part == "" || part == "." || part == ".." {
			return nil, ErrInvalidDocument
		}
		path[i] = url.PathEscape(part)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.Base+"/file/bot"+c.Token+"/"+strings.Join(path, "/"),
		http.NoBody,
	)
	if err != nil {
		return nil, ErrInvalidDocument
	}
	client := http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	if c.HTTP != nil {
		client.Transport = c.HTTP.Transport
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("telegram download unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusInternalServerError ||
		response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout ||
		response.StatusCode == http.StatusUnauthorized {
		wireErr := error(&APIError{Code: response.StatusCode, Description: "file download temporarily unavailable"})
		if response.StatusCode == http.StatusTooManyRequests {
			wireErr = decodeControlRateLimit(response.Body)
		}
		return nil, c.observeControl(ctx, "getFile", wireErr)
	}
	if response.StatusCode != http.StatusOK {
		return nil, ErrInvalidDocument
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxDocumentBytes+1))
	if err != nil {
		return nil, errors.New("telegram download interrupted")
	}
	if len(body) == 0 || len(body) > MaxDocumentBytes {
		return nil, ErrInvalidDocument
	}
	return body, nil
}

func (c Client) Forward(ctx context.Context, chat, source, message int64) (Message, error) {
	var result Message
	err := c.Call(
		ctx,
		"forwardMessage",
		map[string]int64{"chat_id": chat, "from_chat_id": source, "message_id": message},
		&result,
	)
	return result, err
}
