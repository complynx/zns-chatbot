package legacyfood_test

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func TestFoodAggregatePreservesSourceConfirmationPresence(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		name, record string
		rows         int
	}{
		{"absent", `{}`, 1},
		{"explicit-null", `{"payment_confirmed_date":null}`, 2},
	} {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()
			s := foodDatabase(t)
			order := mealOrder(t, s, "alice")
			key := strings.Repeat("b", 64)
			_, err := s.DB.Exec(t.Context(), `INSERT INTO core.legacy_food_import_references
 (source_key,bot_id,event_id,source_kind,source_record_sha256,target_id,source_record)
 VALUES($1,77,'food-event','food',repeat('c',64),$2,$3)`, key, order.ID, source.record)
			require.NoError(t, err)
			_, err = s.DB.Exec(t.Context(), `UPDATE core.food_payments SET status='paid',legacy_source_key=$2
 WHERE order_id=$1 AND kind='meals' AND generation=0`, order.ID, key)
			require.NoError(t, err)
			exported, err := s.Export(t.Context(), "bob", "food-event")
			require.NoError(t, err)
			details, err := csv.NewReader(strings.NewReader(string(exported.Orders))).ReadAll()
			require.NoError(t, err)
			require.Equal(t, []string{"Да", "Нет", "185.00"}, details[1][4:7])
			summary, err := csv.NewReader(strings.NewReader(string(exported.Summary))).ReadAll()
			require.NoError(t, err)
			require.Len(t, summary, source.rows)
			_, err = s.DB.Exec(t.Context(), `INSERT INTO core.food_payments(order_id,kind,generation,status)
 VALUES($1,'meals',1,'pending')`, order.ID)
			require.NoError(t, err)
			exported, err = s.Export(t.Context(), "bob", "food-event")
			require.NoError(t, err)
			summary, err = csv.NewReader(strings.NewReader(string(exported.Summary))).ReadAll()
			require.NoError(t, err)
			require.Len(t, summary, 1)
			current, err := s.Get(t.Context(), "alice", "food-event", order.ID)
			require.NoError(t, err)
			require.Equal(t, legacyfood.Pending, current.MealPayment.Status)
		})
	}
}
