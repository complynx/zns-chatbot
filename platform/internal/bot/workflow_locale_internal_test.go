package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestWorkflowLocalizedCard(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ language, status, confirm, cancel, places string }{
		{"en", "Status: Draft", "Confirm", "Cancel booking", "places 2"},
		{"ru", "Статус: Черновик", "Подтвердить", "Отменить заявку", "мест 2"},
		{"uk", "Статус: Черновик", "Подтвердить", "Отменить заявку", "мест 2"},
	} {
		t.Run(test.language, func(t *testing.T) {
			t.Parallel()
			notice, err := defaultNotice(test.language, stateDraft)
			require.NoError(t, err)
			payload, err := renderPayload(
				test.language,
				101,
				core.Workflow{State: stateDraft, Version: 4, SlotID: "one"},
				[]core.Slot{
					{ID: "one", Title: "Custom service", Price: 30, Currency: "BYN", Remaining: 2},
				},
				notice,
				false,
			)
			require.NoError(t, err)
			assert.Contains(t, payload.Text, test.status)
			assert.Contains(t, payload.Text, "Custom service")
			require.Len(t, payload.Markup.Rows, 3)
			assert.Equal(t, test.confirm, payload.Markup.Rows[0][0].Text)
			assert.Equal(t, "confirm::4", payload.Markup.Rows[0][0].Data)
			assert.Equal(t, test.cancel, payload.Markup.Rows[1][0].Text)
			assert.Equal(t, "cancel::4", payload.Markup.Rows[1][0].Data)
			assert.Contains(t, payload.Markup.Rows[2][0].Text, test.places)
			assert.Equal(t, "select:one:4", payload.Markup.Rows[2][0].Data)
		})
	}
}

func TestWorkflowNativeReplyRemainsUntranslated(t *testing.T) {
	t.Parallel()
	const reply = "**Ответ агента** stays exactly as written"
	payload, err := renderPayload("en", 101, core.Workflow{State: stateBooked}, nil, reply, true)
	require.NoError(t, err)
	assert.Contains(t, payload.Text, "*Ответ агента* stays exactly as written")
	assert.Contains(t, payload.Text, "Status: Booked")
}
