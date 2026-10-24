package render_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestRenderFixtures(t *testing.T) {
	t.Parallel()
	user := telegram.User{ID: 7, FirstName: "Daniel"}
	tests := []struct {
		name     string
		markdown string
		text     string
		entities []telegram.MessageEntity
	}{
		{name: "nested emoji", markdown: "😀 **bold _yes_**", text: "😀 bold yes", entities: []telegram.MessageEntity{
			{Type: "bold", Offset: 3, Length: 8}, {Type: "italic", Offset: 8, Length: 3},
		}},
		{
			name:     "strike",
			markdown: "~~gone~~",
			text:     "gone",
			entities: []telegram.MessageEntity{{Type: "strikethrough", Length: 4}},
		},
		{
			name:     "code literal",
			markdown: "`&amp; **x**`",
			text:     "&amp; **x**",
			entities: []telegram.MessageEntity{{Type: "code", Length: 11}},
		},
		{
			name:     "fence",
			markdown: "```go\nx &amp; y\n```",
			text:     "x &amp; y\n",
			entities: []telegram.MessageEntity{{Type: "pre", Length: 10, Language: "go"}},
		},
		{
			name:     "unfinished fence",
			markdown: "```\nanswer &amp;",
			text:     "answer &amp;\n",
			entities: []telegram.MessageEntity{{Type: "pre", Length: 13}},
		},
		{name: "code inside style", markdown: "**a `x` b**", text: "a x b", entities: []telegram.MessageEntity{
			{Type: "bold", Length: 2}, {Type: "code", Offset: 2, Length: 1}, {Type: "bold", Offset: 3, Length: 2},
		}},
		{
			name:     "link",
			markdown: "[page](https://example.com/a?x=1&y=2)",
			text:     "page",
			entities: []telegram.MessageEntity{{Type: "text_link", Length: 4, URL: "https://example.com/a?x=1&y=2"}},
		},
		{
			name:     "verified mention",
			markdown: "[Daniel](tg://user?id=7)",
			text:     "Daniel",
			entities: []telegram.MessageEntity{{Type: "text_mention", Length: 6, User: &user}},
		},
		{name: "unknown mention", markdown: "[Daniel](tg://user?id=8)", text: "Daniel"},
		{name: "malformed mention", markdown: "[Daniel](tg://user?id=bad)", text: "Daniel"},
		{name: "unfinished markers", markdown: "before **after _", text: "before **after _"},
		{name: "malformed link", markdown: "[label](https://example.com", text: "[label](https://example.com"},
		{name: "html", markdown: "<b>hello</b>", text: "<b>hello</b>"},
		{name: "html block", markdown: "<div>\nhello\n</div>", text: "<div>\nhello\n</div>"},
		{name: "lists", markdown: "- one\n- two", text: "- one\n- two"},
		{name: "ordered", markdown: "3. one\n4. two", text: "3. one\n4. two"},
		{name: "quote", markdown: "> hello", text: "> hello"},
		{
			name:     "heading",
			markdown: "# Hello",
			text:     "Hello",
			entities: []telegram.MessageEntity{{Type: "bold", Length: 5}},
		},
	}
	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			result := render.Render(fixture.markdown, map[int64]telegram.User{7: user})
			require.Len(t, result, 1)
			require.Equal(t, fixture.text, result[0].Text)
			require.Equal(t, fixture.entities, result[0].Entities)
			require.NoError(t, render.Validate(result[0]))
		})
	}
}

func TestMentionIdentityCannotBeReassigned(t *testing.T) {
	t.Parallel()
	result := render.Render("[same name](tg://user?id=7)", map[int64]telegram.User{7: {ID: 8, FirstName: "same name"}})
	require.Len(t, result, 1)
	require.Equal(t, "same name", result[0].Text)
	require.Empty(t, result[0].Entities)
}

func TestSplitPreservesTextAndEntities(t *testing.T) {
	t.Parallel()
	value := strings.Repeat("a", 4095) + "😀" + strings.Repeat("😀", 2100)
	result := render.Render("**"+value+"**", nil)
	require.Len(t, result, 3)
	require.Equal(t, value, joined(result))
	for _, message := range result {
		require.True(t, utf8.ValidString(message.Text))
		require.NoError(t, render.Validate(message))
		require.Equal(
			t,
			[]telegram.MessageEntity{{Type: "bold", Length: len(utf16.Encode([]rune(message.Text)))}},
			message.Entities,
		)
	}
}

func TestPlainPreservesWhitespace(t *testing.T) {
	t.Parallel()
	value := "\n" + strings.Repeat("😀", 3000) + "\n \n"
	require.Equal(t, value, joined(render.Plain(value)))
	require.Empty(t, render.Plain(""))
}

func TestValidateRejectsInvalidEntities(t *testing.T) {
	t.Parallel()
	tests := []render.Message{
		{Text: "😀", Entities: []telegram.MessageEntity{{Type: "bold", Offset: 1, Length: 1}}},
		{Text: "abc", Entities: []telegram.MessageEntity{{Type: "bold", Length: 4}}},
		{
			Text:     "abc",
			Entities: []telegram.MessageEntity{{Type: "bold", Length: 2}, {Type: "italic", Offset: 1, Length: 2}},
		},
		{
			Text:     "abc",
			Entities: []telegram.MessageEntity{{Type: "bold", Length: 3}, {Type: "code", Offset: 1, Length: 1}},
		},
		{
			Text: "abc",
			Entities: []telegram.MessageEntity{
				{Type: "text_link", Length: 3, URL: "https://a"},
				{Type: "text_mention", Length: 2, User: &telegram.User{ID: 7}},
			},
		},
	}
	for _, message := range tests {
		require.ErrorIs(t, render.Validate(message), render.ErrInvalidEntities)
	}
}

func FuzzRender(f *testing.F) {
	for _, value := range []string{
		"😀 **nested _text_**",
		"```\n&amp;",
		"[x](tg://user?id=7)",
		"<div>**x**</div>",
		strings.Repeat("😀", 2100),
		"**a `x` b**",
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, markdown string) {
		for _, message := range render.Render(markdown, map[int64]telegram.User{7: {ID: 7}}) {
			require.NoError(t, render.Validate(message))
			for _, entity := range message.Entities {
				if entity.Type == "text_mention" {
					require.Equal(t, int64(7), entity.User.ID)
				}
			}
		}
		normalized := strings.ToValidUTF8(markdown, "�")
		result := render.Plain(markdown)
		if strings.TrimSpace(normalized) != "" && len(utf16.Encode([]rune(normalized))) <= 4096 {
			require.Equal(t, normalized, joined(result))
		}
		withoutSpaces := func(value string) string {
			return strings.Map(func(char rune) rune {
				if unicode.IsSpace(char) {
					return -1
				}
				return char
			}, value)
		}
		require.Equal(t, withoutSpaces(normalized), withoutSpaces(joined(result)))
		for _, message := range result {
			require.NoError(t, render.Validate(message))
			require.Empty(t, message.Entities)
		}
	})
}

func joined(messages []render.Message) string {
	var output strings.Builder
	for _, message := range messages {
		output.WriteString(message.Text)
	}
	return output.String()
}

func TestSplitPreservesLinkMentionAndCode(t *testing.T) {
	t.Parallel()
	label := strings.Repeat("😀", 2100)
	user := telegram.User{ID: 7, FirstName: "Daniel"}
	cases := []struct {
		name     string
		markdown string
		text     string
		entity   telegram.MessageEntity
	}{
		{
			name:     "link",
			markdown: "[" + label + "](https://example.com)",
			text:     label,
			entity:   telegram.MessageEntity{Type: "text_link", URL: "https://example.com"},
		},
		{
			name:     "mention",
			markdown: "[" + label + "](tg://user?id=7)",
			text:     label,
			entity:   telegram.MessageEntity{Type: "text_mention", User: &user},
		},
		{
			name:     "code",
			markdown: "```go\n" + label + " &amp;\n```",
			text:     label + " &amp;\n",
			entity:   telegram.MessageEntity{Type: "pre", Language: "go"},
		},
	}
	for _, fixture := range cases {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			messages := render.Render(fixture.markdown, map[int64]telegram.User{7: user})
			require.Len(t, messages, 2)
			require.Equal(t, fixture.text, joined(messages))
			for _, message := range messages {
				require.NoError(t, render.Validate(message))
				entity := fixture.entity
				entity.Length = len(utf16.Encode([]rune(message.Text)))
				require.Equal(t, []telegram.MessageEntity{entity}, message.Entities)
			}
		})
	}
}

func TestBlankTextCannotBeSent(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", " \t\n", strings.Repeat(" ", 4100)} {
		require.Empty(t, render.Plain(value))
		require.Empty(t, render.Render(value, nil))
		require.ErrorIs(t, render.Validate(render.Message{Text: value}), render.ErrInvalidEntities)
	}
	require.ErrorIs(t, render.Validate(render.Message{
		Text: " \n", Entities: []telegram.MessageEntity{{Type: "pre", Length: 2}},
	}), render.ErrInvalidEntities)
}

func TestFencedCodeDoesNotEmitBlankSuffix(t *testing.T) {
	t.Parallel()
	for _, value := range []string{strings.Repeat("a", 4096), strings.Repeat("😀", 2048)} {
		messages := render.Render("```go\n"+value+"\n```", nil)
		require.Len(t, messages, 1)
		require.Equal(t, value, messages[0].Text)
		require.Equal(t, []telegram.MessageEntity{{Type: "pre", Length: 4096, Language: "go"}}, messages[0].Entities)
		require.NoError(t, render.Validate(messages[0]))
	}
}

func TestLongIndentationRetainsWhatFits(t *testing.T) {
	t.Parallel()
	value := strings.Repeat(" ", 4096) + "x"
	messages := render.Render("```\n"+value+"\n```", nil)
	require.Len(t, messages, 1)
	require.Equal(t, strings.Repeat(" ", 4095)+"x", joined(messages))
	for _, message := range messages {
		require.NoError(t, render.Validate(message))
	}
}
