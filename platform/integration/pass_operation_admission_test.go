package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
)

func TestPassOperationUnpersistedTargetWithdrawal(t *testing.T) {
	t.Parallel()
	_, service, command, source := successorBatchFixture(t)
	id := saveOperationReference(t, service, command, source)
	query := derivedmutation.PassOperationQuery{ID: id}
	before, err := operationSummaries(t.Context(), service, "visitor", query)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, "not_committed", before[0].Status)
	require.NotNil(t, before[0].Context)
	changeSuccessorState(t, service, command, "target_grant")
	_, err = operationSummaries(t.Context(), service, "visitor", query)
	requireCode(t, err, "pass_operation_unavailable")
	list, err := operationSummaries(t.Context(), service, "visitor", derivedmutation.PassOperationQuery{})
	require.NoError(t, err)
	require.Empty(t, list)
	var batches, receipts int
	require.NoError(t, service.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_batches`).Scan(&batches))
	require.NoError(
		t,
		service.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&receipts),
	)
	require.Zero(t, batches)
	require.Zero(t, receipts)
}
