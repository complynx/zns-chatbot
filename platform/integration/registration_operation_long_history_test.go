package integration_test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

const operationHistoryRows = 20000
const operationHistoryCanary = "PRIVATE-LONG-HISTORY-OUTCOME"

// Long history measures the existing ledger query and typed/domain boundary.
// Timings are evidence, not an invented acceptance latency or storage redesign.
func TestRegistrationOperationLongHistory(t *testing.T) {
	t.Parallel()
	db, service, command, source := successorBatchFixture(t)
	interruptSuccessorBatch(t, service, command, source)
	originalID := saveOperationReference(t, service, command, source)
	_, err := db.Exec(t.Context(), `UPDATE bot.interactions SET created_at=now()-interval '2 days'
 WHERE owner='visitor' AND kind='script_runs'`)
	require.NoError(t, err)
	ledger := agenthost.ScriptStore{DB: db}
	operationHistoryRead(t, ledger, "visitor", originalID)
	seedOperationLongHistory(t, ledger)

	// A later saved copy must not rebind the oldest admission or its source.
	rebound := command
	rebound.Key = "rebound-private-command"
	replay, err := json.Marshal([]any{map[string]any{"calls": []any{map[string]any{
		"pass":    map[string]any{"id": originalID, "name": "passes.batch.assign", "batch": rebound},
		"source":  map[string]any{"generation": 999},
		"outcome": map[string]any{"result": operationHistoryCanary},
	}}}})
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('visitor',900001,'script_runs',$1)`, replay)
	require.NoError(t, err)
	retiredID := rand.Text()
	retiredCommand := command
	retiredCommand.Key, retiredCommand.Recipients = "", nil
	tombstone, err := json.Marshal([]any{map[string]any{"pass_redacted": true, "calls": []any{map[string]any{
		"pass": map[string]any{"id": retiredID, "name": "passes.batch.assign", "batch": retiredCommand},
	}}}})
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('visitor',900002,'script_runs',$1)`, tombstone)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `ANALYZE bot.interactions`)
	require.NoError(t, err)

	recent := operationHistoryRead(t, ledger, "visitor", "")
	require.Len(t, recent, 20)
	require.Equal(t, retiredID, recent[0].ID)
	require.True(t, recent[0].Retired)
	for position, admission := range recent[1:] {
		require.Equal(t, operationHistoryID(operationHistoryRows-position), admission.ID)
		require.NotEqual(t, originalID, admission.ID, "replay cannot move an old original into the recent window")
	}
	for _, number := range []int{1, operationHistoryRows / 2, operationHistoryRows} {
		id := operationHistoryID(number)
		exact := operationHistoryRead(t, ledger, "visitor", id)
		require.Len(t, exact, 1)
		require.Equal(t, id, exact[0].ID)
		require.NotNil(t, exact[0].Menu)
		require.Equal(t, "dance", exact[0].Menu.Event)
	}
	original := operationHistoryRead(t, ledger, "visitor", originalID)
	require.Len(t, original, 1)
	require.Equal(t, command, *original[0].Batch)
	require.Equal(t, source, *original[0].Source)
	request, admittedSource, err := ledger.ReadRegistrationOperation(t.Context(), "visitor", originalID)
	require.NoError(t, err)
	require.Equal(t, command.Key, request.Batch.Key)
	require.Equal(t, source, *admittedSource)
	retired := operationHistoryRead(t, ledger, "visitor", retiredID)
	require.Len(t, retired, 1)
	require.True(t, retired[0].Retired)
	require.Empty(t, retired[0].Batch.Key)
	require.Empty(t, retired[0].Batch.Recipients)
	require.Nil(t, retired[0].Source)
	request, admittedSource, err = ledger.ReadRegistrationOperation(t.Context(), "visitor", retiredID)
	require.Error(t, err)
	require.Nil(t, request)
	require.Nil(t, admittedSource)
	require.Empty(t, operationHistoryRead(t, ledger, "history-other-owner", originalID))
	require.Empty(t, operationHistoryRead(t, ledger, "visitor", rand.Text()))

	// The scenario layer also keeps recent selection bounded while applying live reads.
	started := time.Now()
	summaries, err := operationSummaries(t.Context(), service, "visitor", derivedmutation.PassOperationQuery{})
	t.Logf("long-history domain recent duration=%s returned=%d", time.Since(started), len(summaries))
	require.NoError(t, err)
	require.Len(t, summaries, 19, "retired admission without a receipt witness cannot expose executable intent")
	require.Equal(t, operationHistoryID(operationHistoryRows), summaries[0].ID)
	operationHistoryPrivateAbsent(t, summaries)

	// The same old exact admission still joins canonical committed receipts.
	started = time.Now()
	summaries, err = operationSummaries(t.Context(), service, "visitor",
		derivedmutation.PassOperationQuery{ID: originalID})
	t.Logf("long-history domain exact duration=%s", time.Since(started))
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, originalID, summaries[0].ID)
	require.Equal(t, 1, summaries[0].Committed)
	require.Equal(t, 1, summaries[0].Pending)
	operationHistoryPrivateAbsent(t, summaries)
	changeSuccessorState(t, service, command, "grant")
	_, err = operationSummaries(t.Context(), service, "visitor", derivedmutation.PassOperationQuery{ID: originalID})
	requireCode(t, err, "pass_operation_unavailable")
	original = operationHistoryRead(t, ledger, "visitor", originalID)
	require.Equal(t, command.Key, original[0].Batch.Key, "current-rights denial cannot rewrite the admitted key")
}

func operationHistoryRead(t *testing.T, ledger agenthost.ScriptStore, owner, id string,
) []interaction.RegistrationOperation {
	t.Helper()
	started := time.Now()
	result, err := ledger.ReadRegistrationOperations(t.Context(), owner, id)
	t.Logf("ledger owner=%s exact=%q duration=%s returned=%d", owner, id, time.Since(started), len(result))
	require.NoError(t, err)
	require.LessOrEqual(t, len(result), 20)
	operationHistoryPrivateAbsent(t, result)
	return result
}

func operationHistoryPrivateAbsent(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), operationHistoryCanary)
	require.NotContains(t, string(encoded), "NEVER-RETURN-SCRIPT-BODY")
	require.NotContains(t, string(encoded), "rebound-private-command")
}

func operationHistoryID(number int) string {
	// The admission ID contract permits A-Z and 2-7, with length 26.
	return strings.Map(func(char rune) rune {
		if char >= '0' && char <= '9' {
			return 'A' + char - '0'
		}
		return char
	}, fmt.Sprintf("AAAAAAAAAAAAAAAAAAAA%06d", number))
}

func seedOperationLongHistory(t *testing.T, ledger agenthost.ScriptStore) {
	t.Helper()
	_, err := ledger.DB.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content,created_at)
 SELECT owner,100000+number,'script_runs',jsonb_build_array(jsonb_build_object('calls',jsonb_build_array(
 jsonb_build_object('pass',jsonb_build_object('id',translate('AAAAAAAAAAAAAAAAAAAA'||lpad(number::text,6,'0'),
 '0123456789','ABCDEFGHIJ'),'name','passes.registration.show','menu',jsonb_build_object('event','dance','view','home')),
 'outcome',jsonb_build_object('result',$2::text||repeat('x',512)))))),
 now()-interval '1 day'+number*interval '1 second'
 FROM generate_series(1,$1::integer) number
 CROSS JOIN (VALUES('visitor'),('history-other-owner')) owners(owner)`, operationHistoryRows, operationHistoryCanary)
	require.NoError(t, err)
	// Non-operation runs must not displace admissions or require host record decoding.
	_, err = ledger.DB.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content,created_at)
 SELECT 'visitor',200000+number,'script_runs',jsonb_build_array(jsonb_build_object('source','not-a-typed-source',
 'calls',jsonb_build_array(jsonb_build_object('outcome',jsonb_build_object('result',$2::text))))),now()
 FROM generate_series(1,$1::integer) number`, operationHistoryRows, operationHistoryCanary)
	require.NoError(t, err)
	t.Logf("synthetic ledger: %d owner admissions, %d foreign admissions, %d owner non-operation runs",
		operationHistoryRows, operationHistoryRows, operationHistoryRows)
}
