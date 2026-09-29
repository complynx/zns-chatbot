package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestPassMenuRevocationTombstoneSurvivesRestoredAuthority(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := archivedPassFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			run := runPassVM(
				t,
				f,
				72400,
				101,
				"Show my archived payment",
				`await tools.passes.registration.read({event:"archive",view:"payment"}); return tools.passes.registration.show({event:"archive",view:"payment"});`,
			)
			require.Empty(t, run.Error)
			before := passMenuCard(t, f, 101)
			var original string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT (state->'source')::text FROM bot.pass_views WHERE owner='alice'`).
					Scan(&original),
			)
			require.Contains(t, original, `"archive"`)
			require.Contains(t, original, `"created_at"`)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET version=version+1 WHERE event_id='archive' AND owner='alice'`,
			)
			require.NoError(t, err)
			require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, ""))
			text, err := i18n.Translate(language, i18n.RegistrationUnavailable, nil)
			require.NoError(t, err)
			redacted := passMenuCard(t, f, 101)
			require.Equal(t, before.ID, redacted.ID)
			require.Equal(t, text, redacted.Text)
			require.Empty(t, redacted.Markup.Rows)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET version=version-1 WHERE event_id='archive' AND owner='alice'`,
			)
			require.NoError(t, err)
			require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, ""))
			require.Equal(t, text, passMenuCard(t, f, 101).Text)
			var source string
			var tombstone bool
			var buttons int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT (state->'source')::text,(state->>'redacted')::boolean FROM bot.pass_views WHERE owner='alice'`).
					Scan(&source, &tombstone),
			)
			require.Equal(t, original, source)
			require.True(t, tombstone)
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice'`).Scan(&buttons),
			)
			require.Zero(t, buttons)
		})
	}
}
