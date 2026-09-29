package integration_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestMediaDisplayedChoiceAndExplicitTargetSurviveContextBudget(t *testing.T) {
	t.Parallel()
	f := setup(t)
	intakeOrder(t, f, "first")
	intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN",
	}}
	handle(t, f.b, photo)
	// Other unresolved questions remain valid, but must not hide a displayed
	// receipt choice or an explicitly referenced older attachment.
	for id := 101; id <= 112; id++ {
		_, err := f.db.Exec(t.Context(), `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,status)
		SELECT $1,owner,$2,attachment_id,'purpose' FROM bot.media_intake WHERE id='tg-media-100'`,
			"tg-media-"+strconv.Itoa(id), id)
		require.NoError(t, err)
	}
	f.model.plan = agent.Plan{View: "workflow", Text: "Acknowledged."}
	handle(t, f.b, message(200, 101, "The second order for that receipt."))
	assertMediaHintPresent(t, f.model.input, "tg-media-100")
	handle(t, f.b, message(201, 101, "Describe the purpose of tg-media-101."))
	assertMediaHintPresent(t, f.model.input, "tg-media-101")
	assert.LessOrEqual(t, len(f.model.input.MediaContext.Pending), 10)
}

func assertMediaHintPresent(t *testing.T, input agent.Input, id string) {
	t.Helper()
	require.NotNil(t, input.MediaContext)
	var ids []string
	for _, hint := range input.MediaContext.Pending {
		ids = append(ids, hint.ID)
	}
	assert.Contains(t, ids, id)
}
