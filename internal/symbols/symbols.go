// Package symbols translates Telegram visual symbols into bounded descriptive text.
package symbols

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

const Sticker = "sticker"
const Emoji = "emoji"
const MaxOccurrences = 100
const MaxDescription = 400

var ErrInvalid = errors.New("invalid Telegram visual symbols")

// Reference contains transport identifiers. Never include it in conversational input.
type Reference struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	FileID      string `json:"file_id,omitempty"`
	ThumbnailID string `json:"thumbnail_id,omitempty"`
	SetName     string `json:"set_name,omitempty"`
	Animated    bool   `json:"animated,omitempty"`
	Video       bool   `json:"video,omitempty"`
}

type Occurrence struct {
	Reference

	Offset int `json:"offset"`
	Length int `json:"length"`
}

// Description distinguishes literal writing and decorative/connecting fragments from emotion.
type Description struct {
	Text        string `json:"description"`
	Category    string `json:"category"`
	Literal     string `json:"literal,omitempty"`
	Composition string `json:"composition,omitempty"`
	Reaction    string `json:"reaction_hint,omitempty"`
}

func (d Description) Valid() bool {
	if !utf8.ValidString(d.Text+d.Literal+d.Composition+d.Reaction) || strings.TrimSpace(d.Text) == "" ||
		len([]rune(d.Text)) > MaxDescription || len([]rune(d.Literal)) > 100 || len([]rune(d.Composition)) > 200 ||
		len([]rune(d.Reaction)) > 160 {
		return false
	}
	switch d.Category {
	case "emotion", "object", "text", "ornament", "fragment", "unknown":
		return true
	}
	return false
}

func (r Reference) Valid() bool {
	if r.Kind != Sticker && r.Kind != Emoji || len(r.ID) == 0 || len(r.ID) > 512 || len(r.FileID) > 512 ||
		len(r.ThumbnailID) > 512 || len(r.SetName) > 64 {
		return false
	}
	return r.Kind != Emoji || len(r.ID) <= 32 && strings.Trim(r.ID, "0123456789") == ""
}

func FromMessage(m *telegram.Message) ([]Occurrence, error) {
	if m.Sticker != nil {
		s := m.Sticker
		r := Reference{
			Kind:     Sticker,
			ID:       s.UniqueID,
			FileID:   s.FileID,
			SetName:  s.SetName,
			Animated: s.IsAnimated,
			Video:    s.IsVideo,
		}
		if s.Thumbnail != nil {
			r.ThumbnailID = s.Thumbnail.FileID
		}
		if !r.Valid() || r.FileID == "" {
			return nil, ErrInvalid
		}
		return []Occurrence{{Reference: r}}, nil
	}
	text, entities := m.Text, m.Entities
	if m.Caption != "" {
		text, entities = m.Caption, m.CaptionEntities
	}
	var result []Occurrence
	for _, e := range entities {
		if e.Type != "custom_emoji" {
			continue
		}
		r := Reference{Kind: Emoji, ID: e.CustomEmojiID}
		if !r.Valid() {
			return nil, ErrInvalid
		}
		result = append(result, Occurrence{Reference: r, Offset: e.Offset, Length: e.Length})
	}
	if len(result) > MaxOccurrences {
		return nil, ErrInvalid
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Offset < result[j].Offset })
	if err := validateSpans(text, result); err != nil {
		return nil, err
	}
	return result, nil
}

func validateSpans(text string, items []Occurrence) error {
	units, bounds := 0, map[int]bool{0: true}
	for _, r := range text {
		units += utf16.RuneLen(r)
		bounds[units] = true
	}
	previous := 0
	for _, o := range items {
		if o.Kind != Emoji || o.Offset < previous || o.Length <= 0 || o.Offset > units || o.Length > units-o.Offset ||
			!bounds[o.Offset] || !bounds[o.Offset+o.Length] {
			return ErrInvalid
		}
		previous = o.Offset + o.Length
	}
	return nil
}

// Replace preserves order, whitespace and line breaks, including adjacent ornament fragments.
// Unknown symbols never inherit the unrelated Unicode fallback's meaning.
func Replace(text string, items []Occurrence, lookup func(Reference) (Description, bool)) (string, error) {
	if len(items) == 0 {
		return text, nil
	}
	if items[0].Kind == Sticker {
		if len(items) != 1 {
			return "", ErrInvalid
		}
		return "Current user message: a sticker expressing a possible reaction. " +
			tag(items[0].Reference, lookup), nil
	}
	if err := validateSpans(text, items); err != nil {
		return "", err
	}
	units := utf16.Encode([]rune(text))
	var out strings.Builder
	at := 0
	for _, o := range items {
		out.WriteString(string(utf16.Decode(units[at:o.Offset])))
		out.WriteString(tag(o.Reference, lookup))
		at = o.Offset + o.Length
	}
	out.WriteString(string(utf16.Decode(units[at:])))
	return out.String(), nil
}

func tag(r Reference, lookup func(Reference) (Description, bool)) string {
	d, ok := lookup(r)
	if !ok || !d.Valid() {
		d = Description{Text: "Visual description not available yet; meaning unknown.", Category: "unknown"}
	}
	raw, _ := json.Marshal(d)
	name := "custom-emoji"
	if r.Kind == Sticker {
		name = "sticker"
		raw, _ = json.Marshal(struct {
			Description

			Event string `json:"event"`
		}{Event: "sticker_reaction", Description: d})
	}
	return "<" + name + ">" + string(raw) + "</" + name + ">"
}
