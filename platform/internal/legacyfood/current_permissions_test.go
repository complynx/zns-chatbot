package legacyfood_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestReceiptSubmissionRechecksAssignedAdmin(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	order := mealOrder(t, s, "alice")
	order, err := s.Execute(t.Context(), "alice", command(order, "begin_payment", legacyfood.Meals))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: s.DB}).UploadProof(
		t.Context(),
		"alice",
		"receipt.pdf",
		[]byte("synthetic receipt"),
	)
	require.NoError(t, err)
	_, err = s.DB.Exec(t.Context(), `UPDATE core.food_admins SET can_assign=false,can_review=false WHERE owner='bob'`)
	require.NoError(t, err)
	submit := command(order, "submit_proof", legacyfood.Meals)
	submit.ProofID = proof.ID
	_, err = s.Execute(t.Context(), "alice", submit)
	require.ErrorContains(t, err, "food_payment_admin_unavailable")
	unchanged, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, order.Version, unchanged.Version)
	require.Equal(t, legacyfood.Pending, unchanged.MealPayment.Status)
}

func TestLegacyMenuBindingDoesNotOverwriteLaterEdits(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	meals := legacyfood.MealSelection{"friday": {Lunch: &legacyfood.LunchSelection{Type: "no-lunch"}}}
	key := strings.Repeat("a", 64)
	first, err := s.SaveLegacyMenu(t.Context(), "alice", "food-event", key, meals)
	require.NoError(t, err)
	replay, err := s.SaveLegacyMenu(t.Context(), "alice", "food-event", key, meals)
	require.NoError(t, err)
	require.Equal(t, first.Version, replay.Version)
	update := command(first, "toggle_activity", "")
	update.Activity = "open"
	current, err := s.Execute(t.Context(), "alice", update)
	require.NoError(t, err)
	_, err = s.SaveLegacyMenu(t.Context(), "alice", "food-event", key, meals)
	require.ErrorContains(t, err, "stale_version")
	latest, err := s.Get(t.Context(), "alice", "food-event", first.ID)
	require.NoError(t, err)
	require.Equal(t, current.Version, latest.Version)
	require.True(t, latest.Activities["open"])
	_, err = s.SaveLegacyMenu(t.Context(), "alice", "food-event", key, legacyfood.MealSelection{})
	require.ErrorContains(t, err, "idempotency_conflict")
}

func TestMenuOwnerAndReviewerReadScopesRemainDistinct(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	order := mealOrder(t, s, "alice")
	_, err := s.View(t.Context(), "bob", order.EventID, order.ID)
	require.Error(t, err)
	view, err := s.ReviewView(t.Context(), "bob", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, order.ID, view.Order.ID)
	_, err = s.DB.Exec(t.Context(), `UPDATE core.food_admins SET can_review=false WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = s.ReviewView(t.Context(), "bob", order.EventID, order.ID)
	require.Error(t, err)
	_, err = s.View(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
}

func TestMealSaveClearsRoutingWithoutChangingActivityReceipt(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	order := mealOrder(t, s, "alice")
	toggle := command(order, "toggle_activity", "")
	toggle.Activity = "open"
	order, err := s.Execute(t.Context(), "alice", toggle)
	require.NoError(t, err)
	order, err = s.Execute(t.Context(), "alice", command(order, "begin_payment", legacyfood.Activity))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: s.DB}).UploadProof(
		t.Context(),
		"alice",
		"activity.pdf",
		[]byte("synthetic activity receipt"),
	)
	require.NoError(t, err)
	submit := command(order, "submit_proof", legacyfood.Activity)
	submit.ProofID = proof.ID
	order, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	activity := order.ActivityPayment
	order, err = s.Execute(t.Context(), "alice", command(order, "delete_meals", ""))
	require.NoError(t, err)
	require.Empty(t, order.PaymentAdmin)
	require.Equal(t, activity, order.ActivityPayment)
	require.Equal(t, "bob", order.ActivityPayment.Receiver)
	require.True(t, order.Activities["open"])
}
