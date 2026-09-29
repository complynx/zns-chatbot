package tgmarkdown_test

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/tgmarkdown"
)

func TestParseV2ConvertedFormattingAndTargets(t *testing.T) {
	t.Parallel()
	source := "🚀 **bold *inside*** [Даня](tg://user?id=101) [тема](https://t.me/c/123/7/9?thread=7&single)\n\n> quote\n> **bold**\n\n```go\na := `x`\\path\n```"
	encoded, err := tgmarkdown.Convert(source)
	require.NoError(t, err)
	parsed, err := tgmarkdown.ParseV2(encoded)
	require.NoError(t, err)
	assert.Equal(t, "🚀 bold inside Даня тема\n\nquote\nbold\n\na := `x`\\path\n", parsed.Text)
	found := map[string]tgmarkdown.Entity{}
	for _, entity := range parsed.Entities {
		if _, exists := found[entity.Type]; !exists {
			found[entity.Type] = entity
		}
	}
	assert.Equal(t, 3, found["bold"].Offset) // First bold span sorts before the later quote span.
	assert.Equal(t, int64(101), found["text_mention"].UserID)
	assert.Equal(t, "https://t.me/c/123/7/9?thread=7&single", found["text_link"].URL)
	assert.Equal(t, "go", found["pre"].Language)
	assert.Equal(t, 10, found["blockquote"].Length)
}

func TestParseV2RejectsMalformedInput(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"*unclosed", "[link](javascript:alert)", "not escaped!", "*`code`*", "[nested [link](https://x)](https://y)", "\\", ">*unclosed\nnext", "__unclosed", "|", "```bad language\nx\n```"} {
		_, err := tgmarkdown.ParseV2(source)
		require.Error(t, err, source)
	}
}

func TestParseV2EscapesAndUTF16(t *testing.T) {
	t.Parallel()
	cases := []string{"a_*[]()~`>#+-=|{}.!\\🚀", strings.Repeat("🚀", 2048)}
	for _, source := range cases {
		parsed, err := tgmarkdown.ParseV2(tgmarkdown.Escape(source))
		require.NoError(t, err)
		assert.Equal(t, source, parsed.Text)
		assert.Empty(t, parsed.Entities)
		assert.Len(t, utf16.Encode([]rune(parsed.Text)), len(utf16.Encode([]rune(source))))
	}
	parsed, err := tgmarkdown.ParseV2(`[path](https://example.com/a(b\)\\c)`)
	require.NoError(t, err)
	require.Len(t, parsed.Entities, 1)
	assert.Equal(t, `https://example.com/a(b)\c`, parsed.Entities[0].URL)
}
