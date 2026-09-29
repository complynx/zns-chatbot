package tgmarkdown_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/tgmarkdown"
)

func TestConvertFormatting(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			"punctuation",
			"Hello, world! [no link] + - = | {x} (a).",
			`Hello, world\! \[no link\] \+ \- \= \| \{x\} \(a\)\.`,
		},
		{"emphasis", "**bold** *italic* ~~gone~~", "*bold* _italic_ ~gone~"},
		{"nested", "**bold *inside* end**", "*bold _inside_ end*"},
		{"triple", "***both***", "*_both_*"},
		{"nested_same", "*one *two* end*", "_one two end_"},
		{"heading", "# Heading\n\nnext", "*Heading*\n\nnext"},
		{"linebreaks", "one  \ntwo\nthree", "one\ntwo\nthree"},
		{"list", "- One\n- Two", "• One\n• Two"},
		{"ordered", "3. Three\n4. Four", "3\\. Three\n4\\. Four"},
		{"quote", "> **One**\n> two\n\nend", ">*One*\n>two\n\nend"},
		{"html", "<b>not bold</b>", `<b\>not bold</b\>`},
		{"escapes", `\*literal\* and &amp; &#33;`, `\*literal\* and & \!`},
		{"unicode", "**Привет 🚀**", "*Привет 🚀*"},
		{"linked_image", "[outer ![inner](https://image.example)](https://outer.example)",
			"[outer inner](https://outer.example)"},
	}
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			got, err := tgmarkdown.Convert(sample.input)
			require.NoError(t, err)
			assert.Equal(t, sample.want, got)
			_, err = tgmarkdown.ParseV2(got)
			require.NoError(t, err, "converter output must be valid supported MarkdownV2")
		})
	}
}

func TestCodeEscapesAndNesting(t *testing.T) {
	t.Parallel()
	cases := []struct{ input, want string }{
		{"`a_b!`", "`a_b!`"},
		{"``a`b\\c``", "`a\\`b\\\\c`"},
		{"**bold `a_b` bold**", "*bold *`a_b`* bold*"},
		{"[read `code`](https://example.com)", "[read code](https://example.com)"},
		{"> `code`", ">code"},
		{"> ```go\n> x := `a`\n> ```", ">x :\\= \\`a\\`"},
		{"```go\na := `b` \\ path\n```", "```go\na := \\`b\\` \\\\ path\n```"},
		{"```evil\\injection\nx\n```", "```\nx\n```"},
	}
	for _, sample := range cases {
		got, err := tgmarkdown.Convert(sample.input)
		require.NoError(t, err, sample.input)
		assert.Equal(t, sample.want, got, sample.input)
	}
}

func TestLinksPreserveTargets(t *testing.T) {
	t.Parallel()
	targets := []string{
		"tg://user?id=123456789", "https://t.me/daniel", "https://t.me/public_chat", "https://t.me/+privateInvite",
		"https://t.me/c/123456789/321", "https://t.me/public_chat/7/321", "https://t.me/c/123456789/7/321",
		"https://t.me/public_chat/321?thread=7&single", "https://example.com/a%29b?x=1&y=2#anchor",
	}
	for _, target := range targets {
		got, err := tgmarkdown.Convert("[**label**](" + target + ")")
		require.NoError(t, err)
		assert.Equal(t, "[*label*]("+target+")", got)
	}
	got, err := tgmarkdown.Convert(`[path](https://example.com/a\(b\)\\c)`)
	require.NoError(t, err)
	assert.Equal(t, `[path](https://example.com/a(b\)\\c)`, got)
	got, err = tgmarkdown.Convert(`[x](https://t.me/a?x=1&amp;y=2)`)
	require.NoError(t, err)
	assert.Equal(t, "[x](https://t.me/a?x=1&y=2)", got)
	got, err = tgmarkdown.Convert("<https://t.me/channel/42>")
	require.NoError(t, err)
	assert.Equal(t, `[https://t\.me/channel/42](https://t.me/channel/42)`, got)
}

func TestUnsafeLinksFailClosed(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"javascript:alert", "JaVaScRiPt:alert", "data:text/html,x", "file:///etc/passwd", "//example.com", "/relative",
		"tg://resolve?domain=other", "tg://user?id=-1", "tg://user?id=1&id=2", "tg://user?id=1&action=x", "tg://user:80?id=1",
		"https://user:password@example.com", "javascript&#58;alert", "https://example.com/a&#10;b", "mailto:a@example.com",
	} {
		_, err := tgmarkdown.Convert("[label](" + target + ")")
		require.ErrorIs(t, err, tgmarkdown.ErrUnsafeURL, target)
	}
}

func TestBoundsAndLiteralFallback(t *testing.T) {
	t.Parallel()
	_, err := tgmarkdown.Convert(strings.Repeat("a", tgmarkdown.MaxInputBytes+1))
	require.ErrorIs(t, err, tgmarkdown.ErrTooLong)
	_, err = tgmarkdown.Convert(string([]byte{0xff}))
	require.ErrorIs(t, err, tgmarkdown.ErrInvalidText)
	_, err = tgmarkdown.Convert("a\x00b")
	require.ErrorIs(t, err, tgmarkdown.ErrInvalidText)
	_, err = tgmarkdown.Convert(strings.Repeat("> ", 70) + "nested")
	require.ErrorIs(t, err, tgmarkdown.ErrTooLong)
	got, err := tgmarkdown.Convert(strings.Repeat("!", tgmarkdown.MaxInputBytes))
	require.NoError(t, err)
	assert.Len(t, got, 2*tgmarkdown.MaxInputBytes)
	assert.Equal(t, `\_\*\[\]\(\)\~`+"\\`"+`\>\#\+\-\=\|\{\}\.\!\\`, tgmarkdown.Escape("_*[]()~`>#+-=|{}.!\\"))
}
