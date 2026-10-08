package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const nestedOperationCanary = "D005-NESTED-PRIVATE-CONTEXT"

func TestPassOperationNestedMemoWithdrawal(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"allowed", "source", "grant"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := nestedOperationMemos(t)
			withdrawNestedOperation(t, f, change)
			memos, err := f.b.API.Memos(t.Context(), "bob")
			require.NoError(t, err)
			if change == "allowed" {
				require.Len(t, memos, 2)
			} else {
				require.Empty(t, memos, "both generations retain original operation authority")
			}
			result := runPassVM(t, f, 67003, 202, "Read my current notes", `return tools.knowledge.memos();`)
			require.Empty(t, result.Error)
			model := f.b.Model.(*knowledgeModel)
			encoded, err := json.Marshal(model.inputs)
			require.NoError(t, err)
			if change == "allowed" {
				require.Contains(t, string(result.Result), nestedOperationCanary)
				require.Contains(t, string(encoded), nestedOperationCanary)
			} else {
				require.JSONEq(t, `[]`, string(result.Result))
				require.NotContains(
					t,
					string(encoded),
					nestedOperationCanary,
					"retired private carriers must not reach the next model",
				)
			}
			assertNestedOperationStatus(t, f, change)
		})
	}
}

func nestedOperationMemos(t *testing.T) *fixture {
	t.Helper()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Synthetic Name'`)
	require.NoError(t, err)
	assigned := runPassVM(t, f, 67000, 202, "Assign the pass for 101", `
const target = await tools.passes.admin.target({event:"dance",target:"101"});
return tools.passes.admin.assign({event:"dance",target:target.admin_target.booking.owner,
assignment:{create:true,from_profile:true,total_price:150}});`)
	require.Empty(t, assigned.Error)
	first := runPassVM(t, f, 67001, 202, "Remember the operation context", `
const operations = await tools.passes.operations({});
if (operations.length !== 1 || !operations[0].context) throw new Error("contextual receipt required");
await tools.knowledge.memo_read({fact_key:"operation-a"});
return tools.knowledge.memo_set({fact_key:"operation-a",text:"D005-NESTED-PRIVATE-CONTEXT "+JSON.stringify(operations[0])});`)
	require.Empty(t, first.Error)
	second := runPassVM(t, f, 67002, 202, "Copy my operation note", `
const first = await tools.knowledge.memo_read({fact_key:"operation-a"});
await tools.knowledge.memo_read({fact_key:"operation-b"});
return tools.knowledge.memo_set({fact_key:"operation-b",text:first.text});`)
	require.Empty(t, second.Error)
	memos, err := f.b.API.Memos(t.Context(), "bob")
	require.NoError(t, err)
	require.Len(t, memos, 2)
	for _, memo := range memos {
		require.Contains(t, memo.Text, nestedOperationCanary)
		require.Contains(t, memo.Text, "alice")
	}
	return f
}

func withdrawNestedOperation(t *testing.T, f *fixture, change string) {
	t.Helper()
	switch change {
	case "source":
		var sourceID int64
		require.NoError(t, f.db.QueryRow(t.Context(),
			`SELECT id FROM core.conversation_events WHERE owner='bob' AND source_key='tg-user-67000'`).Scan(&sourceID))
		require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "bob", sourceID))
	case "grant":
		_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
		require.NoError(t, err)
	}
}

func assertNestedOperationStatus(t *testing.T, f *fixture, change string) {
	t.Helper()
	service := derivedmutation.Service{DB: f.db, Registration: passbooking.Service{DB: f.db}}
	operations, err := operationSummaries(t.Context(), service, "bob", derivedmutation.PassOperationQuery{})
	require.NoError(t, err)
	if change == "grant" {
		require.Empty(t, operations)
		return
	}
	require.Len(t, operations, 1)
	require.Equal(t, "committed", operations[0].Status)
	if change == "source" {
		require.Nil(t, operations[0].Context)
		require.Equal(t, "unavailable", operations[0].Continuation)
	} else {
		require.NotNil(t, operations[0].Context)
	}
}
