package knowledge_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/internal/knowledge"

	"github.com/google/uuid"
)

func validContent() knowledge.Content {
	return knowledge.Content{
		Slug:     "ЗУК-история",
		Title:    " История зука ",
		Language: " RU ",
		Topics:   []string{" ZOUK ", "история", "zouk"},
		Body:     "# Зук\r\n\r\nТочный текст.  \r\n",
		Sources:  []knowledge.Source{{Label: " Источник ", URL: " https://example.org/source?q=zouk "}},
	}
}

func TestContentNormalizesLabelsPreservesSource(t *testing.T) {
	t.Parallel()
	input := validContent()
	got, err := input.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "зук-история" || got.Title != "История зука" || got.Language != "ru" ||
		strings.Join(got.Topics, ",") != "zouk,история" ||
		got.Body != input.Body ||
		got.Sources[0].URL != "https://example.org/source?q=zouk" {
		t.Fatalf("unexpected content: %+v", got)
	}
	if input.Topics[0] != " ZOUK " || input.Sources[0].Label != " Источник " {
		t.Fatal("normalization mutated input")
	}
	english := knowledge.Content{
		Slug:     "zouk-history",
		Title:    "Zouk history",
		Language: "en",
		Body:     "# History\nOriginal English text.",
	}
	got, err = english.Normalize()
	if err != nil || got.Body != english.Body || got.Language != "en" {
		t.Fatalf("English text changed: %+v, %v", got, err)
	}
}

func TestContentRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*knowledge.Content){
		"empty slug":         func(c *knowledge.Content) { c.Slug = "" },
		"bad slug":           func(c *knowledge.Content) { c.Slug = "a--b" },
		"long slug":          func(c *knowledge.Content) { c.Slug = strings.Repeat("я", 101) },
		"long title":         func(c *knowledge.Content) { c.Title = strings.Repeat("я", 201) },
		"control title":      func(c *knowledge.Content) { c.Title = "two\nlines" },
		"language":           func(c *knowledge.Content) { c.Language = "fr" },
		"blank body":         func(c *knowledge.Content) { c.Body = " \n" },
		"long body":          func(c *knowledge.Content) { c.Body = strings.Repeat("x", knowledge.MaxBodyBytes+1) },
		"invalid utf8":       func(c *knowledge.Content) { c.Body = string([]byte{0xff}) },
		"nul body":           func(c *knowledge.Content) { c.Body = "a\x00b" },
		"many topics":        func(c *knowledge.Content) { c.Topics = make([]string, 13) },
		"blank topic":        func(c *knowledge.Content) { c.Topics = []string{" "} },
		"long topic":         func(c *knowledge.Content) { c.Topics = []string{strings.Repeat("я", 65)} },
		"many sources":       func(c *knowledge.Content) { c.Sources = make([]knowledge.Source, 17) },
		"source credentials": func(c *knowledge.Content) { c.Sources[0].URL = "https://user:secret@example.org" },
		"source scheme":      func(c *knowledge.Content) { c.Sources[0].URL = "file:///tmp/article" },
		"relative URL":       func(c *knowledge.Content) { c.Sources[0].URL = "/article" },
		"long URL":           func(c *knowledge.Content) { c.Sources[0].URL = "https://example.org/" + strings.Repeat("x", 2048) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := validContent()
			change(&c)
			if _, err := c.Normalize(); !errors.Is(err, knowledge.ErrInvalid) {
				t.Fatalf("want invalid, got %v", err)
			}
		})
	}
}

func TestSearchNormalizesIndependentLanguages(t *testing.T) {
	t.Parallel()
	event := uuid.New()
	input := knowledge.SearchRequest{
		Collection: " GENERAL ",
		EventID:    &event,
		Language:   " EN ",
		Topic:      " ЗУК ",
		Queries:    []string{" история ", " zouk history "},
	}
	got, err := input.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got.Collection != knowledge.General || got.EventID == nil || *got.EventID != event ||
		got.Language != "en" ||
		got.Topic != "зук" ||
		got.Queries[0] != "история" ||
		got.Queries[1] != "zouk history" ||
		got.Limit != 10 {
		t.Fatalf("bad filters: %+v", got)
	}
	if input.Queries[0] != " история " {
		t.Fatal("mutated query input")
	}
	defaults, err := (knowledge.SearchRequest{Queries: []string{"zouk"}}).Normalize()
	if err != nil || defaults.Collection != knowledge.All {
		t.Fatalf("defaults: %+v %v", defaults, err)
	}
}

func TestSearchRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	nilID := uuid.Nil
	cases := []knowledge.SearchRequest{
		{Queries: nil},
		{Queries: []string{" "}},
		{Queries: []string{"a", "b", "c", "d"}},
		{
			Queries: []string{strings.Repeat("я", 257)},
		},
		{Queries: []string{"a"}, Limit: -1},
		{Queries: []string{"a"}, Limit: 21},
		{
			Queries:    []string{"a"},
			Collection: "private",
		},
		{Queries: []string{"a"}, Language: "de"},
		{Queries: []string{"a"}, EventID: &nilID},
		{Queries: []string{"a"}, Cursor: strings.Repeat("a", 1025)},
		{Queries: []string{"a"}, Cursor: "a\nb"},
	}
	for i, input := range cases {
		if _, err := input.Normalize(); !errors.Is(err, knowledge.ErrInvalid) {
			t.Errorf("case %d: want invalid, got %v", i, err)
		}
	}
}
