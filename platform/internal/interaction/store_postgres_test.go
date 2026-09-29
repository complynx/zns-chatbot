package interaction_test

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func turnDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_turn_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	return db
}

func TestSavedTurnOwnerWinnerAndTerminalReceipt(t *testing.T) {
	t.Parallel()
	db := turnDatabase(t)
	s := interaction.Store{DB: db}
	plan := interaction.SavedPlan{
		FormatVersion: interaction.CurrentFormatVersion,
		Kind:          interaction.DerivedPlan,
		State:         interaction.Ready,
		Plan:          agent.Plan{Text: "first private answer"},
		PassAuthority: &interaction.PlanAuthority{
			ReadAuthorities: []readsource.Authority{}, Reads: []interaction.PassContextDependency{},
		},
	}
	_, err := s.SaveWinner(t.Context(), "alice", 41, plan)
	require.NoError(t, err)
	plan.Plan.Text = "losing answer"
	winner, err := s.SaveWinner(t.Context(), "alice", 41, plan)
	require.NoError(t, err)
	require.Equal(t, "first private answer", winner.Plan.Text)
	_, err = s.Load(t.Context(), "bob", 41)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = s.SaveWinner(t.Context(), "bob", 41, plan)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('alice',41,'registration_action','{"committed":"true"}'),('alice',41,'reply','"first private answer"')`)
	require.NoError(t, err)
	require.NoError(t, s.MarkTerminal(t.Context(), "alice", 41, 1, interaction.SourceRevoked))
	terminal, err := s.Load(t.Context(), "alice", 41)
	require.NoError(t, err)
	require.Equal(t, interaction.PrivacyTerminal, terminal.State)
	require.Empty(t, terminal.Plan.Text)
	winner, err = s.SaveWinner(t.Context(), "alice", 41, plan)
	require.NoError(t, err)
	require.Equal(t, interaction.PrivacyTerminal, winner.State)
	var receipt bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT content->>'committed'='true' FROM bot.interactions
 WHERE owner='alice' AND update_id=41 AND kind='registration_action'`).Scan(&receipt))
	require.True(t, receipt)
	other, err := s.Load(t.Context(), "bob", 41)
	require.NoError(t, err)
	require.Equal(t, "losing answer", other.Plan.Text)
}

func TestSavedTerminalRetainsTrustedReply(t *testing.T) {
	t.Parallel()
	db := turnDatabase(t)
	s := interaction.Store{DB: db}
	_, err := db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES
 ('alice',42,'reply','"committed receipt"'),('alice',42,'reply_origin','"authoritative"')`)
	require.NoError(t, err)
	require.NoError(t, s.MarkTerminal(t.Context(), "alice", 42, 1, interaction.SourceRevoked))
	notice, err := s.LatestNotice(t.Context(), "alice")
	require.NoError(t, err)
	require.JSONEq(t, `"committed receipt"`, string(notice.Content))
	require.False(t, notice.SourceRevoked)
	require.NoError(t, s.MarkTerminal(t.Context(), "alice", 42, 2, interaction.HistoryDeleted))
	notice, err = s.LatestNotice(t.Context(), "alice")
	require.NoError(t, err)
	require.JSONEq(t, `"committed receipt"`, string(notice.Content))
}

func TestSavedTurnUnsupportedFormatFailsClosed(t *testing.T) {
	t.Parallel()
	for _, payloadVersion := range []string{`payload-'format_version'`, `jsonb_set(payload,'{format_version}','2')`} {
		t.Run(payloadVersion, func(t *testing.T) {
			t.Parallel()
			db := turnDatabase(t)
			s := interaction.Store{DB: db}
			ctx := t.Context()
			plan := publicPlan()
			_, err := s.SaveWinner(ctx, "alice", 91, plan)
			require.NoError(t, err)
			_, err = db.Exec(ctx, `UPDATE interaction.saved_turns SET payload=`+payloadVersion)
			require.NoError(t, err)
			_, err = db.Exec(
				ctx,
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',91,'reply','"retained"')`,
			)
			require.NoError(t, err)
			_, err = s.Load(ctx, "alice", 91)
			require.ErrorIs(t, err, interaction.ErrUnsupportedFormat)
			_, err = s.SaveWinner(ctx, "alice", 91, plan)
			require.ErrorIs(t, err, interaction.ErrUnsupportedFormat)
			_, err = s.LatestNotice(ctx, "alice")
			require.ErrorIs(t, err, interaction.ErrUnsupportedFormat)
			_, err = s.ConsumedVoice(ctx, "alice", "voice", 91)
			require.ErrorIs(t, err, interaction.ErrUnsupportedFormat)
			require.ErrorIs(
				t,
				s.MarkTerminal(ctx, "alice", 91, 1, interaction.SourceRevoked),
				interaction.ErrUnsupportedFormat,
			)
			var retained string
			require.NoError(
				t,
				db.QueryRow(ctx, `SELECT content#>>'{}' FROM bot.interactions WHERE owner='alice' AND update_id=91 AND kind='reply'`).
					Scan(&retained),
			)
			require.Equal(t, "retained", retained)
			_, err = s.Load(ctx, "alice", 91)
			require.ErrorIs(t, err, interaction.ErrUnsupportedFormat)
		})
	}
	db := turnDatabase(t)
	s := interaction.Store{DB: db}
	ctx := t.Context()
	for _, version := range []int{0, 2} {
		plan := publicPlan()
		plan.FormatVersion = version
		_, err := s.SaveWinner(ctx, "alice", 92, plan)
		require.ErrorIs(t, err, interaction.ErrUnsupportedFormat)
	}
	require.NoError(t, s.MarkTerminal(ctx, "alice", 93, 0, interaction.HistoryDeleted))
	terminal, err := s.Load(ctx, "alice", 93)
	require.NoError(t, err)
	require.Equal(t, interaction.CurrentFormatVersion, terminal.FormatVersion)
}
