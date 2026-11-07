// Package knowledge validates communal article content and provides bounded,
// exact-source navigation. Persistence owns authorization and event lifecycle.
package knowledge

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxBodyBytes      = 96 * 1024
	MaxTitleRunes     = 200
	MaxSlugRunes      = 100
	MaxTopics         = 12
	MaxTopicRunes     = 64
	MaxSources        = 16
	MaxSourceURLBytes = 2048
	MaxQueryRunes     = 256
	MaxCursorBytes    = 1024
	DefaultLimit      = 10
	MaxLimit          = 20
)

var ErrInvalid = errors.New("invalid knowledge input")

type Source struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Content struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Language string   `json:"language"`
	Topics   []string `json:"topics"`
	Body     string   `json:"body"`
	Sources  []Source `json:"sources"`
}

// Normalize copies and normalizes labels and filters. Body is preserved byte for
// byte, including whitespace and line endings. URLs are references, never fetched.
func (c Content) Normalize() (Content, error) {
	c.Slug = strings.ToLower(strings.TrimSpace(c.Slug))
	if !slug(c.Slug) {
		return Content{}, invalid("slug must contain 1..100 letters/digits separated by single hyphens")
	}
	c.Title = strings.TrimSpace(c.Title)
	if !label(c.Title, MaxTitleRunes) {
		return Content{}, invalid("title must contain 1..200 characters without control characters")
	}
	c.Language = strings.ToLower(strings.TrimSpace(c.Language))
	if c.Language != "en" && c.Language != "ru" {
		return Content{}, invalid("language must be en or ru")
	}
	if err := validateBody(c.Body); err != nil {
		return Content{}, err
	}
	if strings.TrimSpace(c.Body) == "" {
		return Content{}, invalid("body must not be blank")
	}
	if len(c.Topics) > MaxTopics {
		return Content{}, invalid("topics must contain at most 12 entries")
	}
	topics := make([]string, 0, len(c.Topics))
	seen := map[string]bool{}
	for _, topic := range c.Topics {
		topic = strings.ToLower(strings.TrimSpace(topic))
		if !label(topic, MaxTopicRunes) {
			return Content{}, invalid("topic must contain 1..64 characters without control characters")
		}
		if !seen[topic] {
			topics = append(topics, topic)
			seen[topic] = true
		}
	}
	c.Topics = topics
	if len(c.Sources) > MaxSources {
		return Content{}, invalid("sources must contain at most 16 entries")
	}
	sources := make([]Source, len(c.Sources))
	for i, source := range c.Sources {
		source.Label = strings.TrimSpace(source.Label)
		source.URL = strings.TrimSpace(source.URL)
		if !label(source.Label, MaxTitleRunes) {
			return Content{}, invalid("source label must contain 1..200 characters without control characters")
		}
		u, err := url.Parse(source.URL)
		if err != nil || len(source.URL) > MaxSourceURLBytes || !label(source.URL, MaxSourceURLBytes) ||
			u.Hostname() == "" ||
			(u.Scheme != "https" && u.Scheme != "http") ||
			u.User != nil {
			return Content{}, invalid(
				"source URL must be an absolute HTTP(S) URL without credentials, at most 2048 bytes",
			)
		}
		sources[i] = source
	}
	c.Sources = sources
	return c, nil
}

type Collection string

const (
	All     Collection = "all"
	General Collection = "general"
	Event   Collection = "event"
)

type SearchRequest struct {
	Collection Collection `json:"collection"`
	EventID    *uuid.UUID `json:"event_id,omitempty"`
	Language   string     `json:"language,omitempty"`
	Topic      string     `json:"topic,omitempty"`
	Queries    []string   `json:"queries"`
	Limit      int        `json:"limit,omitempty"`
	Cursor     string     `json:"cursor,omitempty"`
}

func (r SearchRequest) Normalize() (SearchRequest, error) {
	r.Collection = Collection(strings.ToLower(strings.TrimSpace(string(r.Collection))))
	if r.Collection == "" {
		r.Collection = All
	}
	if r.Collection != All && r.Collection != General && r.Collection != Event {
		return SearchRequest{}, invalid("collection must be all, general or event")
	}
	// An event filter can select historical general articles with that origin.
	if r.EventID != nil && *r.EventID == uuid.Nil {
		return SearchRequest{}, invalid("event ID must not be nil UUID")
	}
	if r.EventID != nil {
		id := *r.EventID
		r.EventID = &id
	}
	r.Language = strings.ToLower(strings.TrimSpace(r.Language))
	if r.Language != "" && r.Language != "en" && r.Language != "ru" {
		return SearchRequest{}, invalid("language must be en or ru")
	}
	r.Topic = strings.ToLower(strings.TrimSpace(r.Topic))
	if r.Topic != "" && !label(r.Topic, MaxTopicRunes) {
		return SearchRequest{}, invalid("topic must contain at most 64 characters without controls")
	}
	queries, err := normalizeQueries(r.Queries)
	if err != nil {
		return SearchRequest{}, err
	}
	r.Queries = queries
	if r.Limit == 0 {
		r.Limit = DefaultLimit
	}
	if r.Limit < 1 || r.Limit > MaxLimit {
		return SearchRequest{}, invalid("limit must be 1..20")
	}
	if len(r.Cursor) > MaxCursorBytes || (r.Cursor != "" && !label(r.Cursor, MaxCursorBytes)) {
		return SearchRequest{}, invalid("cursor must contain at most 1024 bytes without controls")
	}
	return r, nil
}

func normalizeQueries(queries []string) ([]string, error) {
	if len(queries) < 1 || len(queries) > 3 {
		return nil, invalid("search requires 1..3 queries")
	}
	normalized := make([]string, len(queries))
	for i, query := range queries {
		query = strings.TrimSpace(query)
		if !label(query, MaxQueryRunes) {
			return nil, invalid("query must contain 1..256 characters without controls")
		}
		normalized[i] = query
	}
	return normalized, nil
}

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func label(value string, maxRunes int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func slug(value string) bool {
	if !label(value, MaxSlugRunes) || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") ||
		strings.Contains(value, "--") {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}
func validateBody(body string) error {
	if len(body) > MaxBodyBytes || !utf8.ValidString(body) || strings.ContainsRune(body, 0) {
		return invalid("body must be valid UTF-8 without NUL, at most 98304 bytes")
	}
	return nil
}
