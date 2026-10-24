package knowledge_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func TestOutlineAndSectionKeepOriginalMarkdown(t *testing.T) {
	t.Parallel()
	body := "Intro\r\n# Зук\r\nOriginal **text**.\r\n```md\r\n# Not a heading\r\n```\r\n## Deta" +
		"il\r\nDetails\r\n# Зук\r\nSecond\r\n# Зук-2\r\nThird\r\n# Зук\r\nFourth\r\n"
	outline, err := knowledge.Outline(body, knowledge.OutlineRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if outline.Total != 5 {
		t.Fatalf("fenced heading leaked: %+v", outline)
	}
	want := []string{"зук", "detail", "зук-2", "зук-2-2", "зук-3"}
	for i, h := range outline.Headings {
		if h.Anchor != want[i] {
			t.Errorf("heading %d anchor %q want %q", i, h.Anchor, want[i])
		}
	}
	first := outline.Headings[0]
	if first.StartLine != 2 || first.EndLine != 8 {
		t.Fatalf("wrong section lines: %+v", first)
	}
	got, err := knowledge.Read(body, knowledge.ReadRequest{Anchor: "зук"})
	if err != nil {
		t.Fatal(err)
	}
	wantText := "# Зук\r\nOriginal **text**.\r\n```md\r\n# Not a heading\r\n```\r\n## Detail\r\nDetails\r\n"
	if got.Text != wantText || got.Text != body[got.StartByte:got.EndByte] || got.NextByte != nil || got.Truncated {
		t.Fatalf("section changed: %+v", got)
	}
	window, err := knowledge.Read(body, knowledge.ReadRequest{StartLine: 3, LineCount: 2})
	if err != nil || window.Text != "Original **text**.\r\n```md\r\n" || window.StartLine != 3 || window.EndLine != 4 ||
		window.NextByte == nil {
		t.Fatalf("line window: %+v %v", window, err)
	}
}

func TestOutlineSetextAndPagination(t *testing.T) {
	t.Parallel()
	body := "A title\n=======\nbody\n\n    # Indented code\n\n# !!!\n\n# !!!\n"
	first, err := knowledge.Outline(body, knowledge.OutlineRequest{Limit: 2})
	if err != nil || first.Total != 3 || len(first.Headings) != 2 || first.Headings[0].Title != "A title" ||
		first.Headings[0].StartLine != 1 ||
		first.Headings[1].Anchor != "section" ||
		!first.Truncated ||
		first.NextIndex == nil {
		t.Fatalf("outline: %+v %v", first, err)
	}
	next, err := knowledge.Outline(body, knowledge.OutlineRequest{StartIndex: *first.NextIndex, Limit: 2})
	if err != nil || len(next.Headings) != 1 || next.Headings[0].Anchor != "section-2" || next.Truncated ||
		next.NextIndex != nil {
		t.Fatalf("next: %+v %v", next, err)
	}
	long, err := knowledge.Outline("# "+strings.Repeat("я", 1000), knowledge.OutlineRequest{})
	if err != nil || len([]rune(long.Headings[0].Title)) != 200 || !long.Headings[0].TitleTruncated ||
		len([]rune(long.Headings[0].Anchor)) > 80 {
		t.Fatalf("unbounded heading: %+v %v", long, err)
	}
}

func TestEmptyHeadingsDelimitExactSections(t *testing.T) {
	t.Parallel()
	for _, newline := range []string{"\n", "\r\n"} {
		for _, emptyHeading := range []string{"#", "#   ", "### ###"} {
			t.Run(strconv.Quote(emptyHeading+newline), func(t *testing.T) {
				t.Parallel()
				firstSection := strings.Join(
					[]string{"### A", "body", "```md", "#", "#   ", "### ###", "```", ""},
					newline,
				)
				emptySection := emptyHeading + newline + "next" + newline
				body := firstSection + emptySection + "# B" + newline + "end"
				outline, err := knowledge.Outline(body, knowledge.OutlineRequest{})
				if err != nil {
					t.Fatal(err)
				}
				if outline.Total != 3 || outline.Headings[1].Anchor != "section" || outline.Headings[1].Title != "" ||
					outline.Headings[1].StartLine != 8 ||
					outline.Headings[1].EndLine != 9 {
					t.Fatalf("empty or fenced heading index: %+v", outline)
				}
				for anchor, want := range map[string]string{"a": firstSection, "section": emptySection} {
					assertExactSection(t, body, anchor, want)
				}
			})
		}
	}
	outline, err := knowledge.Outline("#", knowledge.OutlineRequest{})
	if err != nil || outline.Total != 1 || outline.Headings[0].Anchor != "section" ||
		outline.Headings[0].StartLine != 1 {
		t.Fatalf("empty heading without final newline: %+v, %v", outline, err)
	}
}

func assertExactSection(t *testing.T, body, anchor, want string) {
	t.Helper()
	got, err := knowledge.Read(body, knowledge.ReadRequest{Anchor: anchor})
	if err != nil || got.Text != want || got.Text != body[got.StartByte:got.EndByte] || got.NextByte != nil ||
		got.Truncated {
		t.Fatalf("section %q: %+v, error %v, want %q", anchor, got, err, want)
	}
}

func TestOutlineDenseDuplicateAndCollidingAnchors(t *testing.T) {
	t.Parallel()
	var source strings.Builder
	source.WriteString("# x-2\n# x-2\n")
	for suffix := 3; suffix <= 65; suffix++ {
		source.WriteString("# x-" + strconv.Itoa(suffix) + "\n")
	}
	const duplicates = 20000
	source.WriteString(strings.Repeat("# x\n", duplicates))
	source.WriteString("# x-66\n# x\n# x-2\n")
	body := source.String()
	first, err := knowledge.Outline(body, knowledge.OutlineRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != duplicates+68 || first.Headings[0].Anchor != "x-2" || first.Headings[1].Anchor != "x-2-2" ||
		first.Headings[65].Anchor != "x" ||
		first.Headings[66].Anchor != "x-66" {
		t.Fatalf("colliding anchors: %+v", first)
	}
	last, err := knowledge.Outline(body, knowledge.OutlineRequest{StartIndex: first.Total - 4, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"x-20064", "x-66-2", "x-20065", "x-2-3"}
	for i, heading := range last.Headings {
		if heading.Anchor != want[i] {
			t.Errorf("heading %d anchor %q want %q", i, heading.Anchor, want[i])
		}
	}
	if len(last.Headings) != len(want) || last.Truncated || last.NextIndex != nil {
		t.Fatalf("final outline page: %+v", last)
	}
}

func TestReadLongUnicodeLineResumesWithoutLoss(t *testing.T) {
	t.Parallel()
	body := "# Текст\n" + strings.Repeat("я🙂x", 2000) + "\nLast\n"
	var rebuilt strings.Builder
	var cursor *int
	for range 10000 {
		got, err := knowledge.Read(body, knowledge.ReadRequest{StartByte: cursor, MaxBytes: 17})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Text) > 17 || !utf8.ValidString(got.Text) || got.Text != body[got.StartByte:got.EndByte] ||
			len(got.Text) == 0 {
			t.Fatalf("bad chunk: %+v", got)
		}
		rebuilt.WriteString(got.Text)
		if got.NextByte == nil {
			break
		}
		if *got.NextByte <= got.StartByte {
			t.Fatal("continuation made no progress")
		}
		cursor = got.NextByte
	}
	if rebuilt.String() != body {
		t.Fatalf("lost text: got %d bytes want %d", rebuilt.Len(), len(body))
	}
	part, err := knowledge.Read(body, knowledge.ReadRequest{StartLine: 2, LineCount: 1, MaxBytes: 17})
	if err != nil || part.StartLine != 2 || part.EndLine != 2 || !part.Truncated || part.NextByte == nil {
		t.Fatalf("long line metadata: %+v %v", part, err)
	}
}

func TestFindLiteralUnicodeContextAndContinuation(t *testing.T) {
	t.Parallel()
	body := "Prefix🙂\nЗУК [a.b] хвост\nзук [A.B] final\nnot aXb"
	first, err := knowledge.Find(body, knowledge.FindRequest{Query: "[a.b]", Limit: 1, ContextBytes: 9})
	if err != nil || len(first.Matches) != 1 || !first.Truncated || first.NextByte == nil {
		t.Fatalf("find: %+v %v", first, err)
	}
	match := first.Matches[0]
	if body[match.StartByte:match.EndByte] != "[a.b]" || match.StartLine != 2 || match.EndLine != 2 ||
		match.Excerpt.Text != body[match.Excerpt.StartByte:match.Excerpt.EndByte] ||
		!utf8.ValidString(match.Excerpt.Text) ||
		!strings.Contains(match.Excerpt.Text, "[a.b]") {
		t.Fatalf("match source: %+v", match)
	}
	next, err := knowledge.Find(body, knowledge.FindRequest{Query: "[a.b]", StartByte: *first.NextByte, Limit: 1})
	if err != nil || len(next.Matches) != 1 || next.Truncated ||
		body[next.Matches[0].StartByte:next.Matches[0].EndByte] != "[A.B]" {
		t.Fatalf("find continuation: %+v %v", next, err)
	}
	russian, err := knowledge.Find(body, knowledge.FindRequest{Query: "зук"})
	if err != nil || len(russian.Matches) != 2 {
		t.Fatalf("Russian case matching: %+v %v", russian, err)
	}
	absent, err := knowledge.Find(body, knowledge.FindRequest{Query: "does not exist"})
	if err != nil || len(absent.Matches) != 0 || absent.Truncated || absent.NextByte != nil {
		t.Fatalf("empty find: %+v %v", absent, err)
	}
}

func TestFindAnchorsPreambleAndLaterNestedSections(t *testing.T) {
	t.Parallel()
	body := "needle preamble\n# One\nfirst\n# Two\nsecond\n# Three\nthird\n# Four\nneedle par" +
		"ent\n## Repeat\nneedle nested\n## Repeat\nneedle repeated\n# End\nend"
	result, err := knowledge.Find(body, knowledge.FindRequest{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"", "four", "repeat", "repeat-2"}
	if len(result.Matches) != len(want) {
		t.Fatalf("matches: %+v", result)
	}
	for i, match := range result.Matches {
		if match.Anchor != want[i] || body[match.StartByte:match.EndByte] != "needle" {
			t.Errorf("match %d: %+v, want anchor %q", i, match, want[i])
		}
		if match.Anchor != "" {
			section, readErr := knowledge.Read(body, knowledge.ReadRequest{Anchor: match.Anchor})
			if readErr != nil || match.StartByte < section.StartByte || match.EndByte > section.EndByte {
				t.Errorf("anchor does not open matching section: %+v, %v", section, readErr)
			}
		}
	}
}

func TestReadRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	body := "# A\nя text\n# B\nend"
	midRune := 5
	for _, r := range []knowledge.ReadRequest{{MaxBytes: 3}, {MaxBytes: knowledge.MaxReadBytes + 1}, {LineCount: -1}, {LineCount: 201}, {StartLine: -1}, {StartLine: 100}, {StartLine: 1, StartByte: &midRune}, {StartByte: &midRune}, {Anchor: "missing"}, {Anchor: "b", StartLine: 2}} {
		if _, err := knowledge.Read(body, r); !errors.Is(err, knowledge.ErrInvalid) {
			t.Errorf("read %+v: %v", r, err)
		}
	}
}

func TestFindRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	body := "# A\nя text\n# B\nend"
	midRune := 5
	for _, r := range []knowledge.FindRequest{{Query: ""}, {Query: strings.Repeat("x", 257)}, {Query: "x", StartByte: midRune}, {Query: "x", Limit: 21}, {Query: "x", ContextBytes: 513}, {Query: "x", StartByte: -1}} {
		if _, err := knowledge.Find(body, r); !errors.Is(err, knowledge.ErrInvalid) {
			t.Errorf("find %+v: %v", r, err)
		}
	}
}

func TestOutlineRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	body := "# A\nя text\n# B\nend"
	for _, r := range []knowledge.OutlineRequest{{Limit: 101}, {StartIndex: -1}, {StartIndex: 3}} {
		if _, err := knowledge.Outline(body, r); !errors.Is(err, knowledge.ErrInvalid) {
			t.Errorf("outline %+v: %v", r, err)
		}
	}
}
