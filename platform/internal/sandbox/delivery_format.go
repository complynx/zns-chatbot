package sandbox

import (
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf16"

	"golang.org/x/net/html"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/tgmarkdown"
)

const (
	entityCode     = "code"
	mentionHost    = "user"
	escapeWidth    = 2
	entityPre      = "pre"
	entityStrike   = "strikethrough"
	maxMarkupBytes = 32768
	maxMarkupDepth = 16
)

var errMarkup = errors.New("unsupported or invalid sandbox markup")
var htmlReferences = regexp.MustCompile(`&(?:lt|gt|amp|quot|#[0-9]+|#x[0-9a-fA-F]+);`)
var htmlClosingTag = regexp.MustCompile(`^</[A-Za-z][A-Za-z0-9-]*[ \t\r\n\f]*>$`)
var mentionQuery = regexp.MustCompile(`^id=[1-9][0-9]*$`)

func deliveryText(p telegram.Send) (string, []telegram.MessageEntity, error) {
	if len(p.Text) > maxMarkupBytes {
		return "", nil, errMarkup
	}
	switch p.ParseMode {
	case "HTML":
		return htmlText(p.Text)
	case "Markdown":
		return legacyText(p.Text)
	default:
		return telegram.VisibleText(p)
	}
}

func textUnits(s string) int { return len(utf16.Encode([]rune(s))) }

func safeDeliveryURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	if (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" {
		return true
	}
	return u.Scheme == "tg" && u.Host == mentionHost && u.Path == "" && u.Fragment == "" &&
		mentionQuery.MatchString(u.RawQuery)
}

type htmlEntity struct {
	tag    string
	entity telegram.MessageEntity
}
type htmlDecoder struct {
	output   strings.Builder
	stack    []htmlEntity
	entities []telegram.MessageEntity
	units    int
}

// Tokenization is delegated to x/net/html. Strict validation rejects HTML error
// recovery and tags/attributes that are not in the documented sandbox subset.
func htmlText(source string) (string, []telegram.MessageEntity, error) {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	var decoder htmlDecoder
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if !errors.Is(tokenizer.Err(), io.EOF) || len(decoder.stack) != 0 {
				return "", nil, errMarkup
			}
			text, _, err := telegram.VisibleText(telegram.Send{Text: decoder.output.String()})
			return text, decoder.entities, err
		}
		if err := decoder.step(tokenizer, kind); err != nil {
			return "", nil, err
		}
	}
}

func (d *htmlDecoder) step(tokenizer *html.Tokenizer, kind html.TokenType) error {
	switch kind {
	case html.TextToken:
		raw := string(tokenizer.Raw())
		if strings.ContainsAny(htmlReferences.ReplaceAllString(raw, ""), "<>&") {
			return errMarkup
		}
		text := string(tokenizer.Text())
		d.output.WriteString(text)
		d.units += textUnits(text)
	case html.StartTagToken:
		token := tokenizer.Token()
		entity, err := htmlTag(token)
		if err != nil || !d.canNest(entity) {
			return errMarkup
		}
		entity.Offset = d.units
		d.stack = append(d.stack, htmlEntity{tag: token.Data, entity: entity})
	case html.EndTagToken:
		// Check raw syntax before Token discards invalid closing attributes or slashes.
		if !htmlClosingTag.Match(tokenizer.Raw()) {
			return errMarkup
		}
		token := tokenizer.Token()
		if len(d.stack) == 0 || d.stack[len(d.stack)-1].tag != token.Data {
			return errMarkup
		}
		top := d.stack[len(d.stack)-1]
		d.stack = d.stack[:len(d.stack)-1]
		top.entity.Length = d.units - top.entity.Offset
		if top.entity.Length == 0 {
			return errMarkup
		}
		d.entities = append(d.entities, top.entity)
	case html.ErrorToken, html.SelfClosingTagToken, html.CommentToken, html.DoctypeToken:
		return errMarkup
	}
	return nil
}

func (d *htmlDecoder) canNest(entity telegram.MessageEntity) bool {
	if len(d.stack) >= maxMarkupDepth {
		return false
	}
	for _, parent := range d.stack {
		if parent.entity.Type == entityCode || parent.entity.Type == entityPre || entity.Type == entityCode ||
			entity.Type == entityPre ||
			parent.entity.Type == entity.Type {
			return false
		}
	}
	return true
}

func htmlTag(token html.Token) (telegram.MessageEntity, error) {
	types := map[string]string{
		"b":          "bold",
		"strong":     "bold",
		"i":          "italic",
		"em":         "italic",
		"u":          "underline",
		"ins":        "underline",
		"s":          entityStrike,
		"strike":     entityStrike,
		"del":        entityStrike,
		entityCode:   entityCode,
		entityPre:    entityPre,
		"blockquote": "blockquote",
		"tg-spoiler": "spoiler",
	}
	if token.Data == "a" {
		if len(token.Attr) != 1 || token.Attr[0].Key != "href" || !safeDeliveryURL(token.Attr[0].Val) {
			return telegram.MessageEntity{}, errMarkup
		}
		return telegram.MessageEntity{Type: "text_link", URL: token.Attr[0].Val}, nil
	}
	if token.Data == "span" && len(token.Attr) == 1 && token.Attr[0].Key == "class" &&
		token.Attr[0].Val == "tg-spoiler" {
		return telegram.MessageEntity{Type: "spoiler"}, nil
	}
	if types[token.Data] == "" || len(token.Attr) != 0 {
		return telegram.MessageEntity{}, errMarkup
	}
	return telegram.MessageEntity{Type: types[token.Data]}, nil
}

// Legacy entities are non-nesting. Reuse the project's V2 parser for each
// isolated entity, escaping only the literal text between entities.
func legacyText(source string) (string, []telegram.MessageEntity, error) {
	var converted strings.Builder
	for len(source) > 0 {
		at := strings.IndexAny(source, "_*`[\\")
		if at < 0 {
			converted.WriteString(tgmarkdown.Escape(source))
			break
		}
		converted.WriteString(tgmarkdown.Escape(source[:at]))
		source = source[at:]
		piece, consumed, err := legacyPiece(source)
		if err != nil {
			return "", nil, err
		}
		converted.WriteString(piece)
		source = source[consumed:]
	}
	return telegram.VisibleText(telegram.Send{Text: converted.String(), ParseMode: telegram.MarkdownV2})
}

func legacyPiece(source string) (string, int, error) {
	if source[0] == '\\' {
		if len(source) < 2 || !strings.ContainsRune("_*`[", rune(source[1])) {
			return "", 0, errMarkup
		}
		return tgmarkdown.Escape(source[1:escapeWidth]), escapeWidth, nil
	}
	delimiter := source[:1]
	if strings.HasPrefix(source, "```") {
		delimiter = "```"
	}
	closing := delimiter
	if delimiter == "[" {
		closing = "]("
	}
	end := strings.Index(source[len(delimiter):], closing)
	if end < 0 {
		return "", 0, errMarkup
	}
	end += len(delimiter)
	body := source[len(delimiter):end]
	if delimiter == "`" || delimiter == "```" {
		if strings.ContainsAny(body, "`\\") {
			return "", 0, errMarkup
		}
		return delimiter + body + delimiter, end + len(delimiter), nil
	}
	if strings.ContainsAny(body, "_*`[\\") {
		return "", 0, errMarkup
	}
	if delimiter == "[" {
		return legacyLink(source, end, body)
	}
	return delimiter + tgmarkdown.Escape(body) + delimiter, end + len(delimiter), nil
}

func legacyLink(source string, end int, body string) (string, int, error) {
	tail := source[end+2:]
	closeAt := strings.IndexByte(tail, ')')
	if closeAt < 0 || !safeDeliveryURL(tail[:closeAt]) {
		return "", 0, errMarkup
	}
	return "[" + tgmarkdown.Escape(body) + "](" + tail[:closeAt] + ")", end + 2 + closeAt + 1, nil
}
