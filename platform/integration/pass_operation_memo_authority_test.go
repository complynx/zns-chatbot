package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// A real host admission supplies the canonical witness. Only the operations
// tool can supply current authority to the downstream memo after retirement.
func TestPassOperationOpaqueMemoGrantWithdrawal(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Synthetic Name'`)
	require.NoError(t, err)
	result := runPassVM(t, f, 66000, 202, "Assign pass for 101 using profile and comment PRIVATE-OPERATION-INTENT", `
const target = await tools.passes.admin.target({event:"dance",target:"101"});
return tools.passes.admin.assign({event:"dance",target:target.admin_target.booking.owner,
assignment:{create:true,from_profile:true,total_price:150,comment:"PRIVATE-OPERATION-INTENT"}});`)
	require.Empty(t, result.Error)
	ledger := agenthost.ScriptStore{DB: f.db}
	admissions, err := ledger.ReadRegistrationOperations(t.Context(), "bob", "")
	require.NoError(t, err)
	require.Len(t, admissions, 1)
	require.NotNil(t, admissions[0].Witness)
	require.True(t, admissions[0].Witness.Valid("bob"))
	service := derivedmutation.Service{DB: f.db, Registration: passbooking.Service{DB: f.db}}
	history := conversation.Service{DB: f.db}
	var sourceID int64
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT id FROM core.conversation_events WHERE owner='bob' AND source_key='tg-user-66000'`).Scan(&sourceID))
	require.NoError(t, history.DeleteContent(t.Context(), "bob", sourceID))
	operations, err := operationSummaries(t.Context(), service, "bob", derivedmutation.PassOperationQuery{})
	require.NoError(t, err)
	require.Len(t, operations, 1)
	require.Nil(t, operations[0].Context)
	result = runPassVM(t, f, 66001, 202, "Remember the completed operation status", `
const operations = tools.passes.operations({});
if (operations.length !== 1 || operations[0].context) throw new Error("expected one opaque receipt");
tools.knowledge.memo_read({fact_key:"operation-status"});
return tools.knowledge.memo_set({fact_key:"operation-status",text:JSON.stringify(operations[0])});`)
	require.Empty(t, result.Error, "%+v", result)

	var stored string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions
 WHERE owner='bob' AND update_id=66000 AND kind='script_runs'`).Scan(&stored))
	require.NotContains(t, stored, "PRIVATE-OPERATION-INTENT")
	retired, err := ledger.ReadRegistrationOperations(t.Context(), "bob", admissions[0].ID)
	require.NoError(t, err)
	require.Len(t, retired, 1)
	require.True(t, retired[0].Retired)
	require.Equal(t, admissions[0].Witness, retired[0].Witness)
	before, err := f.b.API.Memos(t.Context(), "bob")
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Contains(t, before[0].Text, "passes.admin.assign")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	after, err := f.b.API.Memos(t.Context(), "bob")
	require.NoError(t, err)
	require.Empty(t, after, "opaque operation facts remain bound to current domain permission")
}
