package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
)

var ErrFileUnavailable = errors.New("telegram file unavailable")
var ErrFileTooLarge = errors.New("telegram file exceeds read limit")

// ReadFile refreshes the temporary path each time and keeps the bot token inside the client.
func (c *Client) ReadFile(ctx context.Context, id string, limit int64) ([]byte, error) {
	if id == "" || limit < 1 {
		return nil, ErrFileUnavailable
	}
	var file struct {
		ID   string `json:"file_id"`
		Size int64  `json:"file_size"`
		Path string `json:"file_path"`
	}
	if err := c.call(ctx, "getFile", struct {
		ID string `json:"file_id"`
	}{id}, &file, false); err != nil {
		var rejected *Error
		if errors.As(err, &rejected) && rejected.fileTooLarge {
			return nil, ErrFileTooLarge
		}
		return nil, err
	}
	if file.ID != id || !validFilePath(file.Path) || file.Size < 0 {
		return nil, ErrFileUnavailable
	}
	if file.Size > limit {
		return nil, ErrFileTooLarge
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/file/bot"+c.token+"/"+file.Path, nil)
	if err != nil {
		return nil, ErrFileUnavailable
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, ErrFileUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, ErrFileUnavailable
	}
	if response.ContentLength > limit {
		return nil, ErrFileTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, ErrFileUnavailable
	}
	if int64(len(raw)) > limit {
		return nil, ErrFileTooLarge
	}
	if file.Size > 0 && int64(len(raw)) != file.Size {
		return nil, ErrFileUnavailable
	}
	return raw, nil
}
func validFilePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return false
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == ".." || segment == "." {
			return false
		}
	}
	for _, ch := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-/.", ch) {
			return false
		}
	}
	return true
}
