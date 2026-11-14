package render_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPeopleReferences(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	user := telegram.User{ID: 7, FirstName: "Даниил [🕺]", LastName: "Дрижук"}
	people := map[string]telegram.User{id: user}
	for _, source := range []string{
		"Нашла " + id,
		"İ 🕺 **" + strings.ToUpper(id) + "**",
		"`" + id + "`",
		"[" + id + "](https://example.com)",
		id + " и " + id,
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			chunks := render.People(source, nil, people)
			require.NotEmpty(t, chunks)
			mentions := 0
			for _, chunk := range chunks {
				require.NoError(t, render.Validate(chunk))
				require.NotContains(t, strings.ToLower(chunk.Text), id)
				require.Contains(t, chunk.Text, user.FirstName+" "+user.LastName)
				for _, entity := range chunk.Entities {
					if entity.Type == "text_mention" {
						mentions++
						require.Equal(t, user.ID, entity.User.ID)
					}
				}
			}
			require.Equal(t, strings.Count(strings.ToLower(source), id), mentions)
		})
	}
}

func TestDebugJSONKeepsOriginalAccountIDs(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	chunks := render.People("```json\n{\"user_id\":\""+id+"\"}\n```", nil,
		map[string]telegram.User{id: {ID: 7, FirstName: "Даниил"}})
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Text, id)
	require.NotContains(t, chunks[0].Text, "Даниил")
	require.NoError(t, render.Validate(chunks[0]))
}

func TestLiteralContentKeepsAccountIDs(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	for _, source := range []string{"```\n" + id + "\n```", "```text\n" + id + "\n```"} {
		chunks := render.People(source, nil, map[string]telegram.User{id: {ID: 7, FirstName: "Даниил"}})
		require.Contains(t, chunks[0].Text, id)
		require.NoError(t, render.Validate(chunks[0]))
	}
}

func TestUnknownAccountReferenceRemainsLiteral(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	chunks := render.People("Unknown "+id, nil, nil)
	require.Equal(t, "Unknown "+id, chunks[0].Text)
	require.Empty(t, chunks[0].Entities)
}

func TestPeopleMentionsSurviveChunking(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	user := telegram.User{ID: 7, FirstName: "Даниил"}
	chunks := render.People(strings.Repeat("🕺 "+id+"\n", 600), nil, map[string]telegram.User{id: user})
	require.Greater(t, len(chunks), 1)
	mentions := 0
	for _, chunk := range chunks {
		require.NoError(t, render.Validate(chunk))
		for _, entity := range chunk.Entities {
			if entity.Type == "text_mention" {
				mentions++
				require.Equal(t, user.ID, entity.User.ID)
			}
		}
	}
	require.GreaterOrEqual(t, mentions, 600)
}
