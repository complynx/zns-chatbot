package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgeEmptyPrivateNotesLocales(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, kind := range []string{"empty", "documents_only"} {
			t.Run(language+"/"+kind, func(t *testing.T) {
				t.Parallel()
				f := setup(t)
				_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice';`, language)
				require.NoError(t, err)
				_, err = f.db.Exec(
					t.Context(),
					`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','curate')`,
				)
				require.NoError(t, err)
				s := knowledge.Service{DB: f.db}
				_, err = s.Execute(
					t.Context(),
					"bob",
					knowledge.Command{
						Name:    knowledge.Curate,
						Key:     "shared",
						Topic:   "travel",
						FactKey: "venue",
						Text:    "Shared fact",
					},
				)
				require.NoError(t, err)
				if kind == "documents_only" {
					_, err = s.Execute(
						t.Context(),
						"alice",
						knowledge.Command{
							Name:    knowledge.DocumentSet,
							Key:     "document",
							Topic:   "travel",
							FactKey: "plan",
							Text:    "Separate topic document",
						},
					)
					require.NoError(t, err)
				}
				handleVisible(t, f.b, message(910, 101, "/knowledge"))
				label, err := i18n.Translate(language, i18n.KnowledgeMemos, nil)
				require.NoError(t, err)
				token, messageID := privateNotesButton(t, f, label)
				handleVisible(t, f.b, aliceCallback(911, messageID, token))
				cards := chatMessages(t, f, 101)
				require.NotEmpty(t, cards)
				empty, err := i18n.Translate(language, i18n.KnowledgeEmpty, nil)
				require.NoError(t, err)
				assert.Equal(t, label+"\n"+empty, cards[len(cards)-1].Text)
				// Navigation stays valid when the empty page is opened again.
				handleVisible(t, f.b, aliceCallback(912, messageID, token))
				cards = chatMessages(t, f, 101)
				assert.Equal(t, label+"\n"+empty, cards[len(cards)-1].Text)
			})
		}
	}
}

func privateNotesButton(t *testing.T, f *fixture, label string) (string, int64) {
	t.Helper()
	for _, card := range chatMessages(t, f, 101) {
		for _, row := range card.Markup.Rows {
			for _, button := range row {
				if button.Text == label {
					return button.Data, card.ID
				}
			}
		}
	}
	t.Fatal("private notes navigation button missing")
	return "", 0
}
