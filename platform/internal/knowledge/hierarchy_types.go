package knowledge

import "time"

const (
	MemoryFactKind     = "fact"
	MemoryMemoKind     = "memo"
	MemoryDocumentKind = "document"
	MemoryShared       = "shared"
	MemoryPrivate      = "private"
	MemoryAll          = "all"
	MemoryLiteral      = "literal"
	MemoryRegex        = "regex"
	MemoryText         = "text"
	memoryScanLimit    = 100
	memorySnippetLimit = 160
)

// MemoryQuery selects evidence, never instructions. Empty namespace means all.
type MemoryQuery struct {
	Namespace string `json:"namespace"`
	Event     string `json:"event"`
	Topic     string `json:"topic"`
	Text      string `json:"text"`
	Mode      string `json:"mode"`
	Cursor    string `json:"cursor"`
}

type MemoryEntry struct {
	Source             *SourceProvenance `json:"source,omitempty"`
	SourceKind         string            `json:"source_kind"`
	More               bool              `json:"more"`
	NextCursor         string            `json:"next_cursor"`
	Offset             int               `json:"offset"`
	TotalCharacters    int               `json:"total_characters"`
	Ref                string            `json:"ref"`
	Namespace          string            `json:"namespace"`
	Event              string            `json:"event"`
	Topic              string            `json:"topic"`
	Key                string            `json:"key"`
	Version            int64             `json:"version"`
	Text               string            `json:"text"`
	Phase              string            `json:"phase"`
	HistoricalFallback bool              `json:"historical_fallback"`
	Untrusted          bool              `json:"untrusted"`
	Historical         bool              `json:"historical"`
	CapturedAt         *time.Time        `json:"captured_at,omitempty"`
	Active             bool              `json:"active"`
}

type MemoryPage struct {
	Entries    []MemoryEntry `json:"entries"`
	More       bool          `json:"more"`
	Incomplete bool          `json:"incomplete"`
	NextCursor string        `json:"next_cursor"`
	Scanned    int           `json:"scanned"`
}

type MemoryTopic struct {
	Namespace string `json:"namespace"`
	Topic     string `json:"topic"`
	Count     int    `json:"count"`
}

// MemoryOverview separates curated summary content from generated navigation.
type MemoryOverview struct {
	Summaries []MemoryEntry `json:"summaries"`
	Topics    []MemoryTopic `json:"topics"`
	More      bool          `json:"more"`
	Untrusted bool          `json:"untrusted"`
}
