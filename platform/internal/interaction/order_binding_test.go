package interaction_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestOrderSummariesKeepNamedAndOnlyEditableOrder(t *testing.T) {
	t.Parallel()
	list := make([]orders.Order, 12)
	for i := range list {
		list[i] = orders.Order{ID: fmt.Sprintf("order-%02d", i), State: "paid"}
	}
	list[0].State = "cash"
	summaries := interaction.OrderSummaries(list, "edit order-01", 1)
	require.Len(t, summaries, 7)
	require.Equal(t, "order-01", summaries[5].ID)
	require.Equal(t, "order-00", summaries[6].ID)
	require.Len(t, interaction.OrderSummaries(list, "", 2), 5)
}
