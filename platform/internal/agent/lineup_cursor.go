package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

const maxLineupCursorBytes = 512

type lineupPosition struct {
	Offset    int       `json:"offset"`
	Now       time.Time `json:"now"`
	QueryHash string    `json:"query_hash"`
}

func (source *LineupSource) lineupPosition(query LineupQuery, now time.Time) (lineupPosition, error) {
	cursor := query.Cursor
	query.Cursor = ""
	encoded, _ := json.Marshal(query)
	hash := sha256.Sum256(encoded)
	position := lineupPosition{Now: now.UTC(), QueryHash: hex.EncodeToString(hash[:])}
	if cursor == "" {
		return position, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(data) <= sha256.Size {
		return lineupPosition{}, errors.New("invalid lineup cursor")
	}
	payload, signature := data[:len(data)-sha256.Size], data[len(data)-sha256.Size:]
	mac := hmac.New(sha256.New, []byte(source.cursorKey))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return lineupPosition{}, errors.New("invalid or stale lineup cursor")
	}
	var decoded lineupPosition
	if json.Unmarshal(payload, &decoded) != nil || decoded.Offset <= 0 || decoded.QueryHash != position.QueryHash {
		return lineupPosition{}, errors.New("lineup cursor does not match query")
	}
	return decoded, nil
}

func (source *LineupSource) lineupCursor(position lineupPosition) string {
	payload, _ := json.Marshal(position)
	mac := hmac.New(sha256.New, []byte(source.cursorKey))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
}
