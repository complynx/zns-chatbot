package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestDeliveryQueueWithSplitBotRole(t *testing.T) {
	t.Parallel()
	// This fixture also verifies that private core memory and history stay denied.
	f := memorySplitRoleFixture(t)
	db := f.b.DB
	settings := queueSettings()
	ref := delivery.Reference{Owner: delivery.Bot, Key: "role-proof", Effect: "send"}
	queueRegister(t, db, ref, delivery.Destination{Chat: "101"}, delivery.Interactive)
	queueTransaction(t, db, func(tx pgx.Tx) {
		heads, err := delivery.Candidates(t.Context(), tx, settings.BotID, 10)
		require.NoError(t, err)
		require.Len(t, heads, 1)
		require.Equal(t, ref, heads[0].Reference)
	})
	pacer, err := delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	admission, err := pacer.Admit(t.Context())
	require.NoError(t, err)
	require.True(t, admission.Ready)
	require.True(t, queueBegin(t, db, ref).Ready)
	queueFinish(t, db, ref, delivery.Outcome{Kind: delivery.Succeeded, MessageID: 123})
	queueTransaction(t, db, func(tx pgx.Tx) {
		heads, readErr := delivery.Candidates(t.Context(), tx, settings.BotID, 10)
		require.NoError(t, readErr)
		require.Empty(t, heads)
	})
	var allowed bool
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT has_table_privilege(current_user, 'core.delivery_queue', 'DELETE')`).Scan(&allowed))
	require.False(t, allowed)
}
