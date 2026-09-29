package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestQAComposedHistoryDraftAfterDeletion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive := conversation.Service{DB: f.db}
	require.NoError(
		t,
		archive.AppendOriginal(t.Context(), "alice", "qa-derived-choice", "user", "orchid_private_deleted"),
	)
	var eventID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='qa-derived-choice'`).
			Scan(&eventID),
	)
	result := runModernContinuation(
		t,
		f,
		90001,
		identity.AliceTelegramID,
		"Prepare a draft with my earlier customer detail",
		fmt.Sprintf(
			`const h=tools.history.read({event_id:%d});let d=tools.orders.choice({operation:"begin",empty:true});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:h.text});return {ref:d.choice_ref};`,
			eventID,
		),
	)
	var draft struct {
		Ref string `json:"ref"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	require.NotEmpty(t, draft.Ref)
	// Older eligible rows ensure the bounded cleanup cannot reach this recent receipt.
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) SELECT 'alice',g,'script_runs','[{"history_generation":0}]'::jsonb FROM generate_series(1000,1999) g`,
	)
	require.NoError(t, err)
	require.NoError(t, archive.DeleteContent(t.Context(), "alice", eventID))
	result = runModernContinuation(
		t,
		f,
		90002,
		identity.AliceTelegramID,
		"Read the prepared draft",
		fmt.Sprintf(
			`try {return tools.orders.choice({operation:"read",part:"choice",choice_ref:%q})}catch(_){return {denied:true}}`,
			draft.Ref,
		),
	)
	assert.NotContains(
		t,
		string(result),
		"orchid_private_deleted",
		"a current-generation read must fence an older derived draft independently of cleanup",
	)
	result = runModernContinuation(
		t,
		f,
		90003,
		identity.AliceTelegramID,
		"Create the prepared order",
		fmt.Sprintf(
			`try {const o=tools.orders.update({name:"create",choice_ref:%q});return {created:o.id}}catch(_){return {denied:true}}`,
			draft.Ref,
		),
	)
	t.Logf("later current-generation model input result=%s", result)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice' AND choice->>'customer'='orchid_private_deleted'`).
			Scan(&count),
	)
	assert.Zero(t, count, "uncommitted deleted-context draft must not authorize a later business mutation")
}
