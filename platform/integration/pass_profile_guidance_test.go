package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestPassRegistrationProfileGuidanceSurvivesRefresh(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true WHERE id='dance';
UPDATE core.pass_profiles SET role='leader',legal_name='Alice Smith',passport='synthetic' WHERE owner='alice'`)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			event, solo, guidance, leader := "Dance", "Register solo", "Complete the required registration details below.", "Leader"
			if language == "ru" {
				event, solo, guidance, leader = "Танцы", "Зарегистрироваться соло", "Заполните обязательные данные для регистрации ниже.", "Лидер"
			}
			handle(t, f.b, message(900, 101, "/passes"))
			handle(t, f.b, passMenuClick(t, f, 101, 901, event))
			choice := passMenuClick(t, f, 101, 902, solo)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_profiles SET role='',legal_name='',passport='',version=version+1 WHERE owner='alice'`,
			)
			require.NoError(t, err)
			handle(t, f.b, choice)
			restarted := *f.b
			require.NoError(t, restarted.RenderPassMenu(t.Context(), "alice", 101, ""))
			card := passMenuCard(t, f, 101)
			assert.Contains(t, card.Text, guidance)
			labels := []string{}
			for _, row := range card.Markup.Rows {
				for _, button := range row {
					labels = append(labels, button.Text)
				}
			}
			assert.Contains(t, labels, leader)
			assert.NotContains(t, labels, solo)
			f.model.plan = agent.Plan{View: "workflow", Text: "Four."}
			handle(t, f.b, message(903, 101, "What is two plus two?"))
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings WHERE owner='alice'`).Scan(&count),
			)
			assert.Zero(t, count)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_profiles SET role='leader',legal_name='Alice Smith',passport='synthetic',version=version+1 WHERE owner='alice'`,
			)
			require.NoError(t, err)
			require.NoError(t, restarted.RenderPassMenu(t.Context(), "alice", 101, ""))
			assert.NotContains(t, passMenuCard(t, f, 101).Text, guidance)
			handle(t, f.b, passMenuClick(t, f, 101, 904, solo))
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings WHERE owner='alice'`).Scan(&count),
			)
			assert.Equal(t, 1, count)
		})
	}
}
