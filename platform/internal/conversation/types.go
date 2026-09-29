// Package conversation stores owner-private messages and sanitized committed
// state changes. Text and model summaries are evidence, never authorization.
package conversation

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const invalidHistory = "history_invalid"

const DefaultRecent = 12
const MaxRecent = 30
const MaxPage = 20
const MaxTextBytes = 5000
const MaxSummaryBytes = 2048

type Service struct{ DB *pgxpool.Pool }
type Event struct {
	ID             int64           `json:"id"`
	Kind           string          `json:"kind"`
	Text           string          `json:"text,omitempty"`
	Details        json.RawMessage `json:"details"`
	Omitted        bool            `json:"omitted"`
	At             time.Time       `json:"at"`
	HasFullText    bool            `json:"has_full_text,omitempty"`
	OmissionReason string          `json:"omission_reason,omitempty"`
}
type Summary struct {
	Version   int64  `json:"version"`
	ThroughID int64  `json:"through_id"`
	Text      string `json:"text"`
}
type Page struct {
	Generation int64   `json:"generation,omitempty"`
	Error      string  `json:"error,omitempty"`
	Events     []Event `json:"events"`
	NextBefore int64   `json:"next_before"`
	More       bool    `json:"more"`
}
type Window struct {
	Generation int64   `json:"generation,omitempty"`
	Recent     []Event `json:"recent"`
	Summary    Summary `json:"summary"`
	Gap        bool    `json:"gap"`
	BeforeID   int64   `json:"before_id"`
}

const MaxBodyBytes = 16 * 1024 * 1024

// MaxChunkCharacters leaves room for JSON escaping, descriptors and cursors.
const MaxChunkCharacters = 4000

type TextChunk struct {
	EventID    int64  `json:"event_id"`
	Digest     string `json:"digest"`
	Offset     int    `json:"offset"`
	Total      int    `json:"total"`
	Text       string `json:"text"`
	More       bool   `json:"more"`
	NextOffset int    `json:"next_offset"`
	Generation int64  `json:"generation"`
	Omitted    bool   `json:"omitted"`
}
type Query struct {
	Before, After int64
	Limit         int
}
