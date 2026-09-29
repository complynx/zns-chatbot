package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestLegacyMassageResumeUnavailableParty(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			f := massageBotFixture(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", locale, false)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.massage_parties SET is_open=true WHERE id='night';
 INSERT INTO core.massage_parties(id,event_id,starts_at,ends_at,tables)
 SELECT 'next',event_id,starts_at+interval '1 day',ends_at+interval '1 day',tables FROM core.massage_parties WHERE id='night';
 INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at)
 SELECT event_id,specialist,starts_at+interval '1 day',ends_at+interval '1 day' FROM core.massage_work`)
			require.NoError(t, err)
			legacyMassageDraft(
				t,
				f.db,
				`{"party":"night","length":2,"page":3,"selected":{"bob":true,"master":false},"choices":{"7":{"slot":8,"specialist":"bob"}}}`,
			)
			service := massage.Service{DB: f.db}
			click := func(id int64, data string) telegram.Update {
				return telegram.Update{
					ID: id,
					Callback: &telegram.Callback{
						ID:      "legacy-resume",
						From:    telegram.User{ID: 101},
						Data:    data,
						Message: telegram.Message{ID: 1, Chat: telegram.Chat{ID: 101, Type: "private"}},
					},
				}
			}
			handle(t, f.b, click(900, "massage|ed|"+legacyDraftID+"|7"))
			before, err := service.LegacyDraft(t.Context(), "alice", "sandbox-festival", legacyDraftID)
			require.NoError(t, err)
			assert.Zero(t, before.Version, "ineligible saved choice must not book or consume the draft")
			resume := click(901, "massage|ed|"+legacyDraftID)
			handle(t, f.b, resume)
			var view string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state->>'view' FROM bot.massage_views WHERE owner='alice'`).
					Scan(&view),
			)
			assert.Equal(t, "legacy", view, "unavailable saved party must not discard the draft screen")
			card := massageCard(t, f, 101)
			assert.Contains(t, card.Text, "57 BYN")
			after, err := service.LegacyDraft(t.Context(), "alice", "sandbox-festival", legacyDraftID)
			require.NoError(t, err)
			assert.EqualValues(t, 1, after.Version)
			assert.Equal(t, before.State, after.State)
			handle(t, f.b, resume)
			replay, err := service.LegacyDraft(t.Context(), "alice", "sandbox-festival", legacyDraftID)
			require.NoError(t, err)
			assert.Equal(t, after, replay)
			next := time.Date(2030, time.October, 3, 21, 0, 0, 0, time.UTC).Format("02.01 15:04")
			handle(t, f.b, massageClick(t, f, 101, 902, next))
			card = massageCard(t, f, 101)
			assert.Contains(t, card.Text, "57 BYN")
			handle(t, f.b, massageClick(t, f, 101, 903, "❌ Master"))
			changed, err := service.LegacyDraft(t.Context(), "alice", "sandbox-festival", legacyDraftID)
			require.NoError(t, err)
			assert.Equal(t, "next", changed.State.Party)
			assert.Equal(t, 2, changed.State.Length)
			assert.Equal(t, 3, changed.State.Page)
			assert.True(t, changed.State.Selected["master"])
			assert.Equal(t, before.State.Choices, changed.State.Choices)
		})
	}
}
