package tgmarkdown

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var ErrMarkdownV2 = errors.New("invalid or unsupported Telegram MarkdownV2")

type Entity struct {
	Type     string
	Offset   int
	Length   int
	URL      string
	UserID   int64
	Language string
}

type Parsed struct {
	Text     string
	Entities []Entity
}

type v2Frame struct {
	marker string
	entity Entity
}

type v2Decoder struct {
	source     string
	index      int
	units      int
	quoteStart int
	out        strings.Builder
	stack      []v2Frame
	entities   []Entity
}

// ParseV2 decodes the bounded MarkdownV2 subset emitted by Convert. It is used
// for delivery length checks and the local Telegram stand, not to parse HTML.
func ParseV2(source string) (Parsed, error) {
	if len(source) > maxOutputBytes {
		return Parsed{}, ErrTooLong
	}
	if !validText(source) {
		return Parsed{}, ErrInvalidText
	}
	decoder := v2Decoder{source: source, quoteStart: -1}
	for decoder.index < len(source) {
		if err := decoder.step(); err != nil {
			return Parsed{}, err
		}
	}
	if len(decoder.stack) != 0 {
		return Parsed{}, ErrMarkdownV2
	}
	decoder.closeQuote()
	slices.SortStableFunc(decoder.entities, func(a, b Entity) int {
		if a.Offset != b.Offset {
			return a.Offset - b.Offset
		}
		return b.Length - a.Length
	})
	return Parsed{Text: decoder.out.String(), Entities: decoder.entities}, nil
}

func (d *v2Decoder) appendRune() {
	r, size := utf8.DecodeRuneInString(d.source[d.index:])
	d.out.WriteRune(r)
	d.units += utf16.RuneLen(r)
	d.index += size
}

func (d *v2Decoder) step() error {
	rest := d.source[d.index:]
	switch rest[0] {
	case '\\':
		if len(rest) < 2 || rest[1] == 0 || rest[1] > 126 {
			return ErrMarkdownV2
		}
		d.index++
		d.appendRune()
	case '>':
		if d.index != 0 && d.source[d.index-1] != '\n' {
			return ErrMarkdownV2
		}
		if d.quoteStart < 0 {
			d.quoteStart = d.units
		}
		d.index++
	case '\n':
		if len(rest) < 2 || rest[1] != '>' {
			if d.quoteStart >= 0 && len(d.stack) > 0 {
				return ErrMarkdownV2
			}
			d.closeQuote()
		}
		d.appendRune()
	case '`':
		return d.code()
	case '*', '_', '~', '|', '[':
		return d.delimiter()
	case ']':
		return d.link()
	default:
		if strings.ContainsRune("()#+-=|{}.!", rune(rest[0])) {
			return ErrMarkdownV2
		}
		d.appendRune()
	}
	return nil
}

func (d *v2Decoder) delimiter() error {
	marker := d.source[d.index : d.index+1]
	kind := map[string]string{"*": "bold", "_": "italic", "~": "strikethrough", "[": "text_link"}[marker]
	if strings.HasPrefix(d.source[d.index:], "__") {
		marker, kind = "__", "underline"
	}
	if strings.HasPrefix(d.source[d.index:], "||") {
		marker, kind = "||", "spoiler"
	}
	if kind == "" {
		return ErrMarkdownV2
	}
	d.index += len(marker)
	if len(d.stack) > 0 && marker != "[" && d.stack[len(d.stack)-1].marker == marker {
		top := d.stack[len(d.stack)-1]
		d.stack = d.stack[:len(d.stack)-1]
		return d.finish(top.entity)
	}
	for _, frame := range d.stack {
		if frame.marker == marker {
			return ErrMarkdownV2
		}
	}
	if len(d.stack) >= maxDepth {
		return ErrTooLong
	}
	d.stack = append(d.stack, v2Frame{marker: marker, entity: Entity{Type: kind, Offset: d.units}})
	return nil
}

func (d *v2Decoder) finish(entity Entity) error {
	entity.Length = d.units - entity.Offset
	if entity.Length > 0 {
		if len(d.entities) >= maxNodes {
			return ErrTooLong
		}
		d.entities = append(d.entities, entity)
	}
	return nil
}

func (d *v2Decoder) closeQuote() {
	if d.quoteStart < 0 {
		return
	}
	if d.units > d.quoteStart {
		d.entities = append(
			d.entities,
			Entity{Type: "blockquote", Offset: d.quoteStart, Length: d.units - d.quoteStart},
		)
	}
	d.quoteStart = -1
}

func (d *v2Decoder) code() error {
	if len(d.stack) != 0 || d.quoteStart >= 0 {
		return ErrMarkdownV2
	}
	marker := "`"
	entity := Entity{Type: "code", Offset: d.units}
	if strings.HasPrefix(d.source[d.index:], "```") {
		marker = "```"
		entity.Type = "pre"
	}
	d.index += len(marker)
	if entity.Type == "pre" {
		end := strings.IndexByte(d.source[d.index:], '\n')
		if end < 0 {
			return ErrMarkdownV2
		}
		entity.Language = d.source[d.index : d.index+end]
		if strings.ContainsAny(entity.Language, " `\\\r\t") {
			return ErrMarkdownV2
		}
		d.index += end + 1
	}
	for d.index < len(d.source) {
		if strings.HasPrefix(d.source[d.index:], marker) {
			d.index += len(marker)
			return d.finish(entity)
		}
		switch d.source[d.index] {
		case '\\':
			if d.index+1 >= len(d.source) || d.source[d.index+1] == 0 || d.source[d.index+1] > 126 {
				return ErrMarkdownV2
			}
			d.index++
		case '`':
			return ErrMarkdownV2
		}
		d.appendRune()
	}
	return ErrMarkdownV2
}

func (d *v2Decoder) link() error {
	if len(d.stack) == 0 || d.stack[len(d.stack)-1].marker != "[" || !strings.HasPrefix(d.source[d.index:], "](") {
		return ErrMarkdownV2
	}
	entity := d.stack[len(d.stack)-1].entity
	d.stack = d.stack[:len(d.stack)-1]
	d.index += 2
	var target strings.Builder
	closed := false
	for d.index < len(d.source) {
		if d.source[d.index] == ')' {
			d.index++
			closed = true
			break
		}
		if d.source[d.index] == '\\' {
			if d.index+1 >= len(d.source) || d.source[d.index+1] == 0 || d.source[d.index+1] > 126 {
				return ErrMarkdownV2
			}
			d.index++
		}
		r, size := utf8.DecodeRuneInString(d.source[d.index:])
		target.WriteRune(r)
		d.index += size
	}
	if !closed {
		return ErrMarkdownV2
	}
	if !SafeURL(target.String()) {
		return ErrUnsafeURL
	}
	entity.URL = target.String()
	parsed, _ := url.Parse(entity.URL)
	if strings.EqualFold(parsed.Scheme, "tg") {
		entity.Type = "text_mention"
		entity.UserID, _ = strconv.ParseInt(parsed.Query().Get("id"), 10, 64)
		entity.URL = ""
	}
	return d.finish(entity)
}
