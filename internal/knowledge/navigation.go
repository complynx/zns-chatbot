package knowledge

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

const (
	DefaultOutlineLimit = 40
	MaxOutlineLimit     = 100
	DefaultReadBytes    = 8192
	MaxReadBytes        = 16384
	DefaultReadLines    = 80
	MaxReadLines        = 200
	DefaultFindLimit    = 5
	MaxFindLimit        = 20
	DefaultContextBytes = 160
	MaxContextBytes     = 512
	// Goldmark's default ATX priority is 600; capture empty headings before it.
	sourceATXPriority = 599
	firstAnchorSuffix = 2
	maxHeadingLevel   = 6
	maxAnchorLetters  = 80
)

// Heading describes a section including its subheadings, until the next
// equal/higher level. Byte ranges are zero-based and half-open; lines are one-based.
type Heading struct {
	Anchor         string `json:"anchor"`
	Title          string `json:"title"`
	TitleTruncated bool   `json:"title_truncated"`
	Level          int    `json:"level"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	StartByte      int    `json:"start_byte"`
	EndByte        int    `json:"end_byte"`
}

type OutlineRequest struct {
	StartIndex int `json:"start_index,omitempty"`
	Limit      int `json:"limit,omitempty"`
}
type OutlineResult struct {
	Headings  []Heading `json:"headings"`
	Total     int       `json:"total"`
	NextIndex *int      `json:"next_index,omitempty"`
	Truncated bool      `json:"truncated"`
}

func Outline(body string, request OutlineRequest) (OutlineResult, error) {
	if err := validateBody(body); err != nil {
		return OutlineResult{}, err
	}
	if request.Limit == 0 {
		request.Limit = DefaultOutlineLimit
	}
	if request.Limit < 1 || request.Limit > MaxOutlineLimit || request.StartIndex < 0 {
		return OutlineResult{}, invalid("outline limit must be 1..100; start_index must be nonnegative")
	}
	headings := headingIndex(body)
	if request.StartIndex > len(headings) {
		return OutlineResult{}, invalid("outline start_index exceeds heading count")
	}
	end := min(len(headings), request.StartIndex+request.Limit)
	result := OutlineResult{
		Headings:  headings[request.StartIndex:end],
		Total:     len(headings),
		Truncated: end < len(headings),
	}
	if result.Truncated {
		result.NextIndex = &end
	}
	return result, nil
}

type ReadRequest struct {
	Anchor    string `json:"anchor,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	// StartByte resumes inside a long line. Do not combine it with StartLine.
	StartByte *int `json:"start_byte,omitempty"`
	LineCount int  `json:"line_count,omitempty"`
	MaxBytes  int  `json:"max_bytes,omitempty"`
}
type Excerpt struct {
	Text      string `json:"text"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	// Truncated reports a byte cap; NextByte also continues a line-window read.
	Truncated bool `json:"truncated"`
	NextByte  *int `json:"next_byte,omitempty"`
}

// Read returns exact source text. Callers must bind offsets to the requested
// immutable revision; these local offsets are not cross-revision cursors.
func Read(body string, request ReadRequest) (Excerpt, error) {
	if err := validateBody(body); err != nil {
		return Excerpt{}, err
	}
	request, err := request.normalize()
	if err != nil {
		return Excerpt{}, err
	}
	lines := lineStarts(body)
	start, stop, err := sectionBounds(body, request.Anchor)
	if err != nil {
		return Excerpt{}, err
	}
	start, err = request.offset(body, lines, start, stop)
	if err != nil {
		return Excerpt{}, err
	}
	line := lineAt(lines, start)
	windowEnd := stop
	if line-1+request.LineCount < len(lines) {
		windowEnd = min(windowEnd, lines[line-1+request.LineCount])
	}
	end := floorBoundary(body, min(windowEnd, start+request.MaxBytes))
	result := excerpt(body, lines, start, end)
	result.Truncated = end < windowEnd
	if end < stop {
		result.NextByte = &end
	}
	return result, nil
}

func (r ReadRequest) normalize() (ReadRequest, error) {
	if r.MaxBytes == 0 {
		r.MaxBytes = DefaultReadBytes
	}
	if r.LineCount == 0 {
		r.LineCount = DefaultReadLines
	}
	if r.MaxBytes < utf8.UTFMax || r.MaxBytes > MaxReadBytes {
		return ReadRequest{}, invalid("max_bytes must be 4..16384")
	}
	if r.LineCount < 1 || r.LineCount > MaxReadLines {
		return ReadRequest{}, invalid("line_count must be 1..200")
	}
	if r.StartLine < 0 || (r.StartByte != nil && r.StartLine != 0) {
		return ReadRequest{}, invalid("use either a positive start_line or a start_byte")
	}
	return r, nil
}

func sectionBounds(body, anchor string) (int, int, error) {
	if anchor == "" {
		return 0, len(body), nil
	}
	for _, heading := range headingIndex(body) {
		if heading.Anchor == anchor {
			return heading.StartByte, heading.EndByte, nil
		}
	}
	return 0, 0, invalid("section anchor does not exist")
}

func (r ReadRequest) offset(body string, lines []int, start, stop int) (int, error) {
	if r.StartByte != nil {
		if *r.StartByte < start || *r.StartByte > stop || !runeBoundary(body, *r.StartByte) {
			return 0, invalid("start_byte must be a UTF-8 boundary within the selected section")
		}
		start = *r.StartByte
	} else if r.StartLine != 0 {
		if r.StartLine > len(lines) {
			return 0, invalid("start_line exceeds source line count")
		}
		position := lines[r.StartLine-1]
		if position < start || position >= stop {
			return 0, invalid("start_line is outside the selected section")
		}
		start = position
	}
	return start, nil
}

type FindRequest struct {
	Query        string `json:"query"`
	StartByte    int    `json:"start_byte,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	ContextBytes int    `json:"context_bytes,omitempty"`
}
type Match struct {
	Anchor    string  `json:"anchor,omitempty"`
	StartByte int     `json:"start_byte"`
	EndByte   int     `json:"end_byte"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Excerpt   Excerpt `json:"excerpt"`
}
type FindResult struct {
	Matches   []Match `json:"matches"`
	Truncated bool    `json:"truncated"`
	NextByte  *int    `json:"next_byte,omitempty"`
}

// Find searches literal text using Unicode case folding, with nonoverlapping
// matches. Regex syntax in Query has no special meaning. Context is raw source.
func Find(body string, request FindRequest) (FindResult, error) {
	if err := validateBody(body); err != nil {
		return FindResult{}, err
	}
	if !label(request.Query, MaxQueryRunes) || strings.TrimSpace(request.Query) == "" {
		return FindResult{}, invalid("find query must contain 1..256 characters without controls")
	}
	if request.Limit == 0 {
		request.Limit = DefaultFindLimit
	}
	if request.ContextBytes == 0 {
		request.ContextBytes = DefaultContextBytes
	}
	if request.Limit < 1 || request.Limit > MaxFindLimit {
		return FindResult{}, invalid("find limit must be 1..20")
	}
	if request.ContextBytes < 0 || request.ContextBytes > MaxContextBytes {
		return FindResult{}, invalid("context_bytes must be 0..512")
	}
	if request.StartByte < 0 || request.StartByte > len(body) || !runeBoundary(body, request.StartByte) {
		return FindResult{}, invalid("find start_byte must be a UTF-8 boundary within the source")
	}
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(request.Query))
	positions := pattern.FindAllStringIndex(body[request.StartByte:], request.Limit+1)
	result := FindResult{
		Matches:   make([]Match, 0, min(len(positions), request.Limit)),
		Truncated: len(positions) > request.Limit,
	}
	lines := lineStarts(body)
	headings := headingIndex(body)
	for _, position := range positions[:min(len(positions), request.Limit)] {
		start, end := position[0]+request.StartByte, position[1]+request.StartByte
		anchor := ""
		heading := sort.Search(len(headings), func(i int) bool { return headings[i].StartByte > start }) - 1
		if heading >= 0 && end <= headings[heading].EndByte {
			anchor = headings[heading].Anchor
		}
		contextStart := ceilBoundary(body, max(0, start-request.ContextBytes))
		contextEnd := floorBoundary(body, min(len(body), end+request.ContextBytes))
		context := excerpt(body, lines, contextStart, contextEnd)
		context.Truncated = contextStart > 0 || contextEnd < len(body)
		result.Matches = append(
			result.Matches,
			Match{
				Anchor:    anchor,
				StartByte: start,
				EndByte:   end,
				StartLine: lineAt(lines, start),
				EndLine:   lineAt(lines, max(start, end-1)),
				Excerpt:   context,
			},
		)
	}
	if result.Truncated {
		next := result.Matches[len(result.Matches)-1].EndByte
		result.NextByte = &next
	}
	return result, nil
}

// Goldmark omits source segments on empty ATX headings. Retain a zero-length
// segment at their source position so they still delimit original-text sections.
type sourceATXHeadingParser struct{ parser.BlockParser }

func (p sourceATXHeadingParser) Open(
	parent ast.Node,
	reader text.Reader,
	context parser.Context,
) (ast.Node, parser.State) {
	_, segment := reader.PeekLine()
	node, state := p.BlockParser.Open(parent, reader, context)
	if heading, ok := node.(*ast.Heading); ok && len(heading.Source()) == 0 {
		heading.AppendSource(text.NewSegment(segment.Start, segment.Start))
	}
	return node, state
}

func headingIndex(body string) []Heading {
	root := parser.New(parser.WithBlockParsers(util.Prioritized[parser.BlockParser](sourceATXHeadingParser{parser.NewATXHeadingParser()}, sourceATXPriority))).
		ParseStringSource(body)
	lines := lineStarts(body)
	headings := make([]Heading, 0)
	anchors := map[string]bool{}
	nextSuffix := map[string]int{}
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := node.(*ast.Heading)
		if !entering || !ok || len(h.Source()) == 0 {
			return ast.WalkContinue, nil
		}
		segments := h.Source()
		var title strings.Builder
		for _, segment := range segments {
			if title.Len() > 0 {
				title.WriteByte(' ')
			}
			title.WriteString(body[segment.Start:segment.Stop])
		}
		text := strings.TrimSpace(title.String())
		base := headingAnchor(text)
		anchor := base
		suffix := max(firstAnchorSuffix, nextSuffix[base])
		for anchors[anchor] {
			anchor = base + "-" + strconv.Itoa(suffix)
			suffix++
		}
		nextSuffix[base] = suffix
		anchors[anchor] = true
		line := lineAt(lines, segments[0].Start)
		short := []rune(text)
		truncated := len(short) > MaxTitleRunes
		if truncated {
			text = string(short[:MaxTitleRunes])
		}
		headings = append(
			headings,
			Heading{
				Anchor:         anchor,
				Title:          text,
				TitleTruncated: truncated,
				Level:          h.Level,
				StartLine:      line,
				StartByte:      lines[line-1],
			},
		)
		return ast.WalkContinue, nil
	})
	// The stack closes a parent section only at its next sibling or ancestor.
	stack := make([]int, 0, maxHeadingLevel)
	for i := range headings {
		for len(stack) > 0 && headings[stack[len(stack)-1]].Level >= headings[i].Level {
			previous := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			headings[previous].EndByte = headings[i].StartByte
		}
		stack = append(stack, i)
	}
	for _, i := range stack {
		headings[i].EndByte = len(body)
	}
	for i := range headings {
		headings[i].EndLine = lineAt(lines, max(headings[i].StartByte, headings[i].EndByte-1))
	}
	return headings
}
func headingAnchor(title string) string {
	var result strings.Builder
	separator := false
	count := 0
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if count >= maxAnchorLetters {
				break
			}
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(r)
			count++
			separator = false
		} else if unicode.IsSpace(r) || r == '-' {
			separator = true
		}
	}
	if result.Len() == 0 {
		return "section"
	}
	return result.String()
}
func lineStarts(body string) []int {
	starts := []int{0}
	for i := range len(body) {
		if body[i] == '\n' && i+1 < len(body) {
			starts = append(starts, i+1)
		}
	}
	return starts
}
func lineAt(starts []int, offset int) int {
	return sort.Search(len(starts), func(i int) bool { return starts[i] > offset })
}
func runeBoundary(body string, offset int) bool {
	return offset == len(body) || (offset >= 0 && offset < len(body) && utf8.RuneStart(body[offset]))
}
func floorBoundary(body string, offset int) int {
	for offset > 0 && !runeBoundary(body, offset) {
		offset--
	}
	return offset
}
func ceilBoundary(body string, offset int) int {
	for offset < len(body) && !runeBoundary(body, offset) {
		offset++
	}
	return offset
}
func excerpt(body string, lines []int, start, end int) Excerpt {
	return Excerpt{
		Text:      body[start:end],
		StartByte: start,
		EndByte:   end,
		StartLine: lineAt(lines, start),
		EndLine:   lineAt(lines, max(start, end-1)),
	}
}
