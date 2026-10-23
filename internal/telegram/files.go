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
var ErrFileNotFound = errors.New("telegram file identifier unavailable")
var ErrFileAccessDenied = errors.New("telegram file access denied")

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
		if rejected, ok := errors.AsType[*Error](err); ok {
			switch {
			case rejected.fileTooLarge:
				return nil, ErrFileTooLarge
			case rejected.fileInvalidID:
				return nil, ErrFileNotFound
			case rejected.fileAccessDenied:
				return nil, ErrFileAccessDenied
			}
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
	if response.StatusCode == http.StatusForbidden {
		return nil, ErrFileAccessDenied
	}
	if response.StatusCode != http.StatusOK {
		// A download-path 404 can be temporary; only an explicit invalid-ID rejection is classified above.
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
