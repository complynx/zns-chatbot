package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
)

const ReadResourceBytes = 1 << 20
const ReadPageBytes = 24 << 10
const ReadPageItems = 20
const ReadExcerptRunes = 160
const readChunkRunes = 4000

type ReadCursor struct {
	Actor    string `json:"actor"`
	Scope    string `json:"scope"`
	Position string `json:"position,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

type ReadPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
	More       bool   `json:"more"`
}

type ReadChunk struct {
	JSON       string `json:"json"`
	NextCursor string `json:"next_cursor"`
	More       bool   `json:"more"`
}

// DecodeReadCursor reads navigation state only. Domain callers authenticate and
// authorize before loading records; a cursor never grants access.
func DecodeReadCursor(raw, actor, scope string) (ReadCursor, error) {
	cursor := ReadCursor{Actor: actor, Scope: scope}
	if raw == "" {
		return cursor, nil
	}
	const maxCursor = 2048
	if len(raw) > maxCursor {
		return cursor, ReadProblem("read_cursor_invalid")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Actor != actor || cursor.Scope != scope ||
		cursor.Offset < 0 {
		return cursor, ReadProblem("read_cursor_invalid")
	}
	return cursor, nil
}

func EncodeReadCursor(cursor ReadCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func ReadProblem(code string) error {
	status := http.StatusBadRequest
	switch code {
	case "read_stale":
		status = http.StatusConflict
	case "read_result_limit":
		status = http.StatusRequestEntityTooLarge
	case "read_cursor_invalid":
	default:
		return errors.New("invalid read problem")
	}
	return &ProblemError{Status: status, Code: code}
}

func JSONReadChunk(value any, cursor ReadCursor) (ReadChunk, error) {
	result := ReadChunk{}
	data, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if len(data) > ReadResourceBytes {
		return result, ReadProblem("read_result_limit")
	}
	return JSONReadDataChunk(data, cursor)
}

// JSONReadDataChunk pages JSON whose total size is owned by the calling domain.
// Use JSONReadChunk for ordinary resources with the shared resource-size limit.
// Output, digest, cursor scope and Unicode boundaries are identical in both paths.
func JSONReadDataChunk(data []byte, cursor ReadCursor) (ReadChunk, error) {
	return jsonReadDataChunk(data, cursor, readChunkRunes)
}

// JSONReadTransportChunk keeps internal HTTP hydration below the one-MiB
// response budget, including the worst-case six-byte JSON escape per rune.
// Agent-visible tool chunks continue to use JSONReadDataChunk.
func JSONReadTransportChunk(data []byte, cursor ReadCursor) (ReadChunk, error) {
	const transportChunkRunes = 64 << 10
	return jsonReadDataChunk(data, cursor, transportChunkRunes)
}

func jsonReadDataChunk(data []byte, cursor ReadCursor, count int) (ReadChunk, error) {
	result := ReadChunk{}
	digest := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(digest[:])
	if cursor.Digest != "" && cursor.Digest != fingerprint {
		return result, ReadProblem("read_stale")
	}
	runes := []rune(string(data))
	if cursor.Offset > len(runes) {
		return result, ReadProblem("read_cursor_invalid")
	}
	end := min(cursor.Offset+count, len(runes))
	result.JSON = string(runes[cursor.Offset:end])
	result.More = end < len(runes)
	if result.More {
		cursor.Offset = end
		cursor.Digest = fingerprint
		result.NextCursor = EncodeReadCursor(cursor)
	}
	return result, nil
}

// NavigationPage receives one lookahead item and preserves the last returned
// ordering boundary when the serialized page or item budget is reached.
func NavigationPage[T any](items []T, cursor ReadCursor, position func(T) string) (ReadPage[T], error) {
	result := ReadPage[T]{Items: []T{}}
	for index, item := range items {
		previous := result
		result.Items = append(result.Items, item)
		result.More = index+1 < len(items)
		result.NextCursor = ""
		if result.More {
			cursor.Position = position(item)
			result.NextCursor = EncodeReadCursor(cursor)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return result, err
		}
		if len(data) > ReadPageBytes {
			if len(previous.Items) == 0 {
				return previous, ReadProblem("read_result_limit")
			}
			return previous, nil
		}
		if len(result.Items) == ReadPageItems {
			return result, nil
		}
	}
	return result, nil
}
