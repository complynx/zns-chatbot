package migrate_test

import (
	"encoding/json"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const configuredEvent = `{"_id":"configured","key":"configured","finish_date":null,"title_long":{"en":"Long event","ru":"Длинное название"},"title_short":{"en":"Short","ru":"Короткое"},"country_emoji":"🇳🇱","thread_channel":-100123,"thread_id":42,"thread_locale":"ru","price":12500,"amount_cap_per_role":-1,"pass_types":[]}`

func TestEventConfigurationImportAndDrift(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, path, resolution, plan := eventInputs(t, configuredEvent)
	require.NotNil(t, plan.Events[0].Candidate)
	c := plan.Events[0].Candidate.Configuration
	assert.True(t, c.OpenEnded)
	assert.Equal(t, "-100123", c.ThreadChannel)
	assert.Equal(t, int32(-1), c.AmountCapPerRole)
	summary, err := migrate.ApplyEvents(t.Context(), dsn, stage, path, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reconciled)
	var config map[string]json.RawMessage
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT to_jsonb(e) FROM core.pass_events e WHERE id='configured'`).Scan(&config),
	)
	assert.JSONEq(t, `{"en":"Short","ru":"Короткое"}`, string(config["short_titles"]))
	assert.Equal(t, `-1`, string(config["amount_cap_per_role"]))
	_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET thread_id=43 WHERE id='configured'`)
	require.NoError(t, err)
	_, err = migrate.ReconcileEvents(t.Context(), dsn, stage, path, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}
