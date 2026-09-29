package migrate_test

import (
	"encoding/json"
	"os"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportedAnnouncementMarkerPresenceAndDeliveryIndependence(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{"null", "false", `{"$date":"2026-09-02T00:00:00Z"}`} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolution := passImportFixture(t, passImportOptions{announcement: marker})
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			var count int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.legacy_pass_announcement_metadata WHERE marker_present AND source_marker=$1::jsonb AND policy='source_marker'`, marker).
					Scan(&count),
			)
			assert.Equal(t, 3, count)
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_registration_announcements WHERE state='pending'`).
					Scan(&count),
			)
			assert.Zero(t, count)
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.pass_registration_announcements SET failure='operator_inspected'`,
			)
			require.NoError(t, err)
			_, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err, "delivery status is not immutable import evidence")
		})
	}
}

func TestImportedAnnouncementAbsenceNeedsExplicitPolicy(t *testing.T) {
	t.Parallel()
	stage, plan, path := passImportInputs(t, false, false)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var resolution migrate.PassResolutions
	require.NoError(t, json.Unmarshal(raw, &resolution))
	resolution.HistoricalAnnouncements = ""
	raw, err = json.Marshal(resolution)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	_, err = migrate.ApplyPasses(t.Context(), "not-a-dsn", stage, plan, path, migrate.DefaultLimits())
	require.EqualError(t, err, "pass_announcement_policy_required")
}

func TestImportedAnnouncementExplicitSourcePolicy(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, path := passImportFixture(t, passImportOptions{announcementChannel: true})
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var resolution migrate.PassResolutions
	require.NoError(t, json.Unmarshal(raw, &resolution))
	resolution.HistoricalAnnouncements = "preserve_source_eligibility"
	raw, err = json.Marshal(resolution)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	applyOrderDependencies(t, dsn, stage)
	_, err = migrate.ApplyPasses(t.Context(), dsn, stage, plan, path, migrate.DefaultLimits())
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_registration_announcements WHERE state='pending' AND channel='@synthetic'`).
			Scan(&count),
	)
	assert.Equal(t, 3, count)
}
