package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestBusinessCapabilitiesFollowCurrentIdentityAndEvent(t *testing.T) {
	t.Parallel()
	f := setup(t)
	model := &knowledgeModel{plans: []agent.Plan{{Text: "Ready", View: "workflow"}}}
	f.b.Model = model
	handle(t, f.b, message(1980, identity.AliceTelegramID, "show available operations"))
	require.Len(t, model.inputs, 1)
	assert.Equal(t, &agent.BusinessCapabilities{CanBook: true}, model.inputs[0].Business)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.order_admins(event_id,owner,country)
	 VALUES('sandbox-festival','alice','be')`)
	require.NoError(t, err)
	handle(t, f.b, message(1981, identity.AliceTelegramID, "show available operations"))
	require.Len(t, model.inputs, 2)
	assert.Equal(t, &agent.BusinessCapabilities{CanBook: true, CanExportOrders: true}, model.inputs[1].Business)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	handle(t, f.b, message(1982, identity.AliceTelegramID, "show available operations"))
	require.Len(t, model.inputs, 3)
	assert.Equal(t, &agent.BusinessCapabilities{}, model.inputs[2].Business)
	// A later refresh must not rewrite evidence retained for an earlier request.
	assert.True(t, model.inputs[1].Business.CanExportOrders)
}
