package migrate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyDatabase(t *testing.T) (string, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("TEST_DATABASE_URL required for real PostgreSQL apply tests")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	suffix := make([]byte, 8)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "zns_import_test_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.Database = name
	conn, err := pgx.ConnectConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		assert.NoError(t, conn.Close(ctx))
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
		assert.NoError(t, admin.Close(ctx))
	})
	paths, err := filepath.Glob("../../platform/internal/store/migrations/*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		_, execErr := conn.Exec(t.Context(), string(data))
		require.NoError(t, execErr, filepath.Base(path))
	}
	// ConnString retains the original database, so generate the isolated DSN from
	// structured fields rather than accidentally returning the admin database.
	isolatedURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		Path:     "/" + name,
		RawQuery: "sslmode=disable",
	}
	isolated := isolatedURL.String()
	return isolated, conn
}

func applyInputs(t *testing.T, records ...string) (string, string, string) {
	t.Helper()
	stage := usersStage(t, records...)
	plan := filepath.Join(t.TempDir(), "users.jsonl")
	summary, err := migrate.PlanUsers(stage, plan, migrate.DefaultLimits())
	require.NoError(t, err)
	resolutions := migrate.UserResolutions{Version: 1, PlanSHA256: summary.ArtifactSHA256, IdentityAttested: true}
	for _, row := range userRows(t, plan) {
		if row.Status != "candidate" {
			continue
		}
		allowed := false
		resolutions.Users = append(
			resolutions.Users,
			migrate.UserResolution{
				LegacyKey: row.Legacy.Key,
				Owner:     "import-" + row.Legacy.Key,
				Issuer:    "https://synthetic.invalid",
				Subject:   row.Legacy.Key,
				CanBook:   &allowed,
			},
		)
	}
	data, err := json.Marshal(resolutions)
	require.NoError(t, err)
	links := filepath.Join(t.TempDir(), "links.json")
	require.NoError(t, os.WriteFile(links, data, 0o600))
	return stage, plan, links
}

func TestApplyUsersPreservesMetadataReplayAndDetectsDrift(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, links := applyInputs(
		t,
		`{"_id":"synthetic-one","bot_id":77,"user_id":101,"print_name":"Visible Synthetic","first_name":"First","last_name":null,"username":null,"role":"leader","legal_name":"Legal Synthetic","passport_number":"SYNTHETIC","legal_name_frozen":false,"language_code":"by-BY"}`,
		`{"_id":"excluded","bot_id":88,"user_id":102}`,
	)
	summary, err := migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Applied)
	assert.EqualValues(t, 1, summary.Excluded)
	assert.True(t, summary.Reconciled)
	var language, username, lastName, name, legal string
	var frozen, canBook bool
	var metadataUpdate int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT u.language,u.username,u.last_name,u.name,u.can_book,u.telegram_metadata_update,p.legal_name,p.frozen FROM core.users u JOIN core.pass_profiles p ON p.owner=u.id`).
			Scan(&language, &username, &lastName, &name, &canBook, &metadataUpdate, &legal, &frozen),
	)
	assert.Equal(t, "by-BY", language)
	assert.Empty(t, username)
	assert.Empty(t, lastName)
	assert.Equal(t, "Visible Synthetic", name)
	assert.Equal(t, "Legal Synthetic", legal)
	assert.True(t, frozen)
	assert.False(t, canBook)
	assert.EqualValues(t, -1, metadataUpdate)
	summary, err = migrate.ReconcileUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 0, summary.Applied)
	assert.EqualValues(t, 1, summary.Reused)
	_, err = db.Exec(t.Context(), `UPDATE core.users SET name='Later runtime edit'`)
	require.NoError(t, err)
	summary, err = migrate.ReconcileUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
	assert.False(t, summary.Reconciled)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT name FROM core.users`).Scan(&name))
	assert.Equal(t, "Later runtime edit", name)
}

func TestApplyUsersConflictRollsBackWholeDomain(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, links := applyInputs(
		t,
		`{"_id":"first","bot_id":77,"user_id":101,"print_name":"First"}`,
		`{"_id":"second","bot_id":77,"user_id":102,"print_name":"Second"}`,
	)
	rows := userRows(t, plan)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name) VALUES('existing',999,'Existing')`)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES('existing','https://synthetic.invalid',$1)`,
		rows[1].Legacy.Key,
	)
	require.NoError(t, err)
	summary, err := migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_identity_conflict")
	assert.Zero(t, summary.Applied)
	assert.False(t, summary.Reconciled)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_profiles`).Scan(&count))
	assert.Zero(t, count)
	_, err = db.Exec(t.Context(), `DELETE FROM core.zitadel_identities WHERE owner='existing'`)
	require.NoError(t, err)
	summary, err = migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 2, summary.Applied)
	assert.Zero(t, summary.Reused)
	assert.True(t, summary.Reconciled)
}

func TestApplyUsersBlocksUnresolvedDataBeforeDatabase(t *testing.T) {
	t.Parallel()
	stage, plan, links := applyInputs(
		t,
		`{"_id":"blocked","bot_id":77,"user_id":101,"print_name":"Synthetic","banned":false}`,
	)
	_, err := migrate.ApplyUsers(t.Context(), "not-a-dsn", stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_record_blocked")
}

func TestApplyUsersRejectsChangedPlanBeforeDatabase(t *testing.T) {
	t.Parallel()
	stage, plan, links := applyInputs(t, `{"_id":"synthetic","bot_id":77,"user_id":101,"print_name":"Synthetic"}`)
	require.NoError(t, os.WriteFile(plan, []byte("{}\n"), 0o600))
	_, err := migrate.ApplyUsers(t.Context(), "not-a-dsn", stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_plan_mismatch")
}

func TestApplyUsersRejectsUnsafeResolutionsBeforeDatabase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*migrate.UserResolutions)
		want   string
	}{
		{
			"attestation",
			func(r *migrate.UserResolutions) { r.IdentityAttested = false },
			"resolution_attestation_required",
		},
		{"policy", func(r *migrate.UserResolutions) { r.Users[0].CanBook = nil }, "resolution_invalid"},
		{
			"issuer",
			func(r *migrate.UserResolutions) { r.Users[0].Issuer = "http://synthetic.invalid" },
			"resolution_invalid",
		},
		{
			"duplicate key",
			func(r *migrate.UserResolutions) { r.Users[1].LegacyKey = r.Users[0].LegacyKey },
			"resolution_duplicate",
		},
		{
			"duplicate owner",
			func(r *migrate.UserResolutions) { r.Users[1].Owner = r.Users[0].Owner },
			"resolution_duplicate",
		},
		{
			"duplicate identity",
			func(r *migrate.UserResolutions) { r.Users[1].Subject = r.Users[0].Subject },
			"resolution_duplicate",
		},
		{
			"plan binding",
			func(r *migrate.UserResolutions) { r.PlanSHA256 = strings.Repeat("0", 64) },
			"resolution_plan_mismatch",
		},
		{"missing", func(r *migrate.UserResolutions) { r.Users = r.Users[:1] }, "resolution_missing"},
		{
			"extra",
			func(r *migrate.UserResolutions) { r.Users[1].LegacyKey = strings.Repeat("0", 64) },
			"resolution_missing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, plan, links := applyInputs(
				t,
				`{"_id":"one","bot_id":77,"user_id":101,"print_name":"One"}`,
				`{"_id":"two","bot_id":77,"user_id":102,"print_name":"Two"}`,
			)
			raw, err := os.ReadFile(links)
			require.NoError(t, err)
			var resolutions migrate.UserResolutions
			require.NoError(t, json.Unmarshal(raw, &resolutions))
			tc.change(&resolutions)
			raw, err = json.Marshal(resolutions)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(links, raw, 0o600))
			_, err = migrate.ApplyUsers(t.Context(), "not-a-dsn", stage, plan, links, migrate.DefaultLimits())
			require.EqualError(t, err, tc.want)
		})
	}
}

func TestApplyUsersRejectsDuplicateJSONKeys(t *testing.T) {
	t.Parallel()
	stage, plan, links := applyInputs(t, `{"_id":"one","bot_id":77,"user_id":101,"print_name":"One"}`)
	raw, err := os.ReadFile(links)
	require.NoError(t, err)
	raw = []byte(
		strings.Replace(
			string(raw),
			`"identity_attested":true`,
			`"identity_attested":false,"identity_attested":true`,
			1,
		),
	)
	require.NoError(t, os.WriteFile(links, raw, 0o600))
	_, err = migrate.ApplyUsers(t.Context(), "not-a-dsn", stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "resolution_invalid")
}

func TestApplyUsersRejectsExistingAccountAndChangedReceipt(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, links := applyInputs(t, `{"_id":"one","bot_id":77,"user_id":101,"print_name":"One"}`)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name) VALUES('existing',101,'One')`)
	require.NoError(t, err)
	summary, err := migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_target_conflict")
	assert.Zero(t, summary.Applied)
	_, err = db.Exec(t.Context(), `DELETE FROM core.users WHERE id='existing'`)
	require.NoError(t, err)
	_, err = migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.NoError(t, err)
	raw, err := os.ReadFile(links)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(links, append(raw, '\n'), 0o600))
	summary, err = migrate.ReconcileUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_receipt_conflict")
	assert.False(t, summary.Reconciled)
}
