package integration_test

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestProductRoleOwnsSavedTurnsAndStartNotice(t *testing.T) {
	t.Parallel()
	app, meter := productInteractionRoles(t)
	s := interaction.Store{DB: app}
	_, err := app.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('alice',1,'reply','""'),('alice',1,'reply_origin','"authoritative"')`)
	require.NoError(t, err)
	notice, err := s.LatestNotice(t.Context(), "alice")
	require.NoError(t, err, "manual /start reads the joined projection before a saved turn exists")
	require.JSONEq(t, `""`, string(notice.Content))
	require.EqualValues(t, 1, notice.UpdateID)
	_, err = s.LatestNotice(t.Context(), "bob")
	require.ErrorIs(t, err, pgx.ErrNoRows)

	plan := interaction.SavedPlan{
		FormatVersion: interaction.CurrentFormatVersion,
		Kind:          interaction.DerivedPlan,
		State:         interaction.Ready,
		Plan:          agent.Plan{Text: "original answer"},
		PassAuthority: &interaction.PlanAuthority{
			ReadAuthorities: []readsource.Authority{}, Reads: []interaction.PassContextDependency{},
		},
	}
	_, err = s.SaveWinner(t.Context(), "alice", 2, plan)
	require.NoError(t, err)
	plan.Plan.Text = "losing answer"
	winner, err := s.SaveWinner(t.Context(), "alice", 2, plan)
	require.NoError(t, err)
	require.Equal(t, "original answer", winner.Plan.Text)
	_, err = s.Load(t.Context(), "bob", 2)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, s.MarkTerminal(t.Context(), "alice", 2, 1, interaction.SourceRevoked))
	terminal, err := s.Load(t.Context(), "alice", 2)
	require.NoError(t, err)
	require.Equal(t, interaction.PrivacyTerminal, terminal.State)
	require.Empty(t, terminal.Plan.Text)
	notice, err = s.LatestNotice(t.Context(), "alice")
	require.NoError(t, err)
	require.True(t, notice.SourceRevoked)
	assertProductInteractionDenied(t, meter)
}

func assertProductInteractionDenied(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	for _, query := range []string{
		`SELECT payload FROM interaction.saved_turns`,
		`UPDATE interaction.saved_turns SET payload=payload`,
		`DELETE FROM interaction.saved_turns`,
	} {
		_, err := db.Exec(t.Context(), query)
		var denied *pgconn.PgError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "42501", denied.Code)
	}
}

// Run the checked-in bootstrap before migrations, with unique login names so
// this proof neither inherits nor changes roles used by other integration tests.
func productInteractionRoles(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("set TEST_DATABASE_URL to a disposable PostgreSQL cluster")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	suffix := strings.ToLower(rand.Text())
	name, appRole, meterRole := "synthetic_r46_"+suffix, "r46_app_"+suffix, "r46_meter_"+suffix
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	config := admin.Config()
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		ctx := context.WithoutCancel(t.Context())
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		assert.NoError(t, dropErr)
		for _, role := range []string{appRole, meterRole} {
			_, dropErr = admin.Exec(ctx, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
			assert.NoError(t, dropErr)
		}
	})
	raw, err := os.ReadFile("../sandbox/product-roles.sql")
	require.NoError(t, err)
	bootstrap := strings.NewReplacer("zns_app", appRole, "zns_meter", meterRole).Replace(string(raw))
	_, err = db.Exec(t.Context(), bootstrap)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	return productInteractionLogin(t, config, appRole, "sandbox-product-only"),
		productInteractionLogin(t, config, meterRole, "sandbox-meter-only")
}

func productInteractionLogin(t *testing.T, config *pgxpool.Config, role, password string) *pgxpool.Pool {
	t.Helper()
	login := config.Copy()
	login.ConnConfig.User, login.ConnConfig.Password = role, password
	db, err := pgxpool.NewWithConfig(t.Context(), login)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	var current string
	require.NoError(t, db.QueryRow(t.Context(), "SELECT current_user").Scan(&current))
	require.Equal(t, role, current)
	return db
}
