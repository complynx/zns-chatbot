package render_test

import (
	"strings"
	"testing"
	"unicode/utf16"

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

func TestLiteralContentKeepsAccountIDs(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	for _, source := range []string{"```\n" + id + "\n```", "```text\n" + id + "\n```",
		"```json\n{\"user_id\":\"" + id + "\"}\n```"} {
		chunks := render.People(source, nil, map[string]telegram.User{id: {ID: 7, FirstName: "Даниил"}})
		require.Len(t, chunks, 1)
		require.Contains(t, chunks[0].Text, id)
		require.NotContains(t, chunks[0].Text, "Даниил")
		require.NoError(t, render.Validate(chunks[0]))
	}
}

func TestLinkLabelCannotChangePersonIdentity(t *testing.T) {
	t.Parallel()
	const victim = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	for _, destination := range []string{"tg://user?id=8", "https://t.me/attacker"} {
		chunks := render.People("["+victim+"]("+destination+")",
			map[int64]telegram.User{8: {ID: 8, FirstName: "Attacker"}},
			map[string]telegram.User{victim: {ID: 7, FirstName: "Victim"}})
		require.Len(t, chunks, 1)
		require.Equal(t, victim, chunks[0].Text)
		require.NoError(t, render.Validate(chunks[0]))
		require.Len(t, chunks[0].Entities, 1)
		entity := chunks[0].Entities[0]
		if destination == "tg://user?id=8" {
			require.Equal(t, "text_mention", entity.Type)
			require.EqualValues(t, 8, entity.User.ID)
		} else {
			require.Equal(t, "text_link", entity.Type)
			require.Equal(t, destination, entity.URL)
		}
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
	coveredUnits := 0
	for _, chunk := range chunks {
		require.NoError(t, render.Validate(chunk))
		require.NotContains(t, chunk.Text, id)
		for _, entity := range chunk.Entities {
			if entity.Type == "text_mention" {
				mentions++
				coveredUnits += entity.Length
				require.Equal(t, user.ID, entity.User.ID)
			}
		}
	}
	require.GreaterOrEqual(t, mentions, 600)
	// One occurrence can span chunks; coverage must not be lost or duplicated.
	require.Equal(t, 600*len(utf16.Encode([]rune(user.FirstName))), coveredUnits)
}
