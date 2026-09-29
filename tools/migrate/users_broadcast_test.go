package migrate_test

import (
	"encoding/json"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastProjectionPreservesPresenceAndReplay(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, links := applyInputs(
		t,
		`{"_id":"projection","bot_id":77,"user_id":101,"print_name":"Visible","username":null,"last_name":"","inner_name_pt-BR":"Nome","known_names":["A","B"],"informal_name":null}`,
	)
	_, err := migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	var sourceKey, sourceHash string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT fields,source_key,source_hash FROM core.admin_broadcast_profiles`).
			Scan(&fields, &sourceKey, &sourceHash),
	)
	assert.JSONEq(t, `null`, string(fields["username"]))
	assert.JSONEq(t, `""`, string(fields["last_name"]))
	assert.JSONEq(t, `"Nome"`, string(fields["inner_name_pt-BR"]))
	assert.JSONEq(t, `["A","B"]`, string(fields["known_names"]))
	assert.NotContains(t, fields, "first_name")
	assert.NotContains(t, fields, "_id")
	assert.NotEmpty(t, sourceKey)
	assert.Len(t, sourceHash, 64)
	_, err = db.Exec(t.Context(), `UPDATE core.admin_broadcast_profiles SET overrides='{"informal_name":"Current"}'`)
	require.NoError(t, err)
	replayed, err := migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, replayed.Reused)
	var overrides string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT overrides::text FROM core.admin_broadcast_profiles`).Scan(&overrides),
	)
	assert.JSONEq(t, `{"informal_name":"Current"}`, overrides)
	_, err = db.Exec(t.Context(), `UPDATE core.admin_broadcast_profiles SET fields=fields-'username'`)
	require.NoError(t, err)
	_, err = migrate.ApplyUsers(t.Context(), dsn, stage, plan, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_broadcast_profile_reconciliation_failed")
}
