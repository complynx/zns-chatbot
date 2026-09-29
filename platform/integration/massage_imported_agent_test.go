package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestMassageImportedDraftDoesNotTrapGenericAgentText(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	legacyMassageDraft(t, f.db, `{"party":"","length":2,"page":0,"selected":{},"choices":{}}`)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('imported-massage','2035-01-01Z');
 INSERT INTO core.massage_events(id) VALUES('imported-massage');
 UPDATE core.legacy_massage_import_references SET event_id='imported-massage';
 UPDATE core.legacy_massage_drafts SET event_id='imported-massage'`,
	)
	require.NoError(t, err)
	f.b.OrderEventID = "imported-massage"
	model := &recordingModel{plan: agent.Plan{View: "workflow", Text: "Ordinary request handled."}}
	f.b.Model = model
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_events WHERE id='imported-massage'`).Scan(&count),
	)
	require.Zero(t, count)
	service := massage.Service{DB: f.db}
	before, err := service.LegacyDraft(t.Context(), "alice", "imported-massage", legacyDraftID)
	require.NoError(t, err)
	handle(t, f.b, message(950, 101, "Please answer an ordinary question"))
	assert.Equal(t, 1, model.calls)
	assert.Equal(t, "Please answer an ordinary question", model.input.Text)
	after, err := service.LegacyDraft(t.Context(), "alice", "imported-massage", legacyDraftID)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	messages := chatMessages(t, f, 101)
	require.NotEmpty(t, messages)
	assert.Contains(t, messages[len(messages)-1].Text, "Ordinary request handled.")
}
