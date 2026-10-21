package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/orders"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func mealApprovalPreparation(t *testing.T, locale string) (paymentFixture, *peers, uuid.UUID, db.OrderPreparation) {
	t.Helper()
	f, p, _, args := mealOrderAppFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	catalog, err := f.H.store.OwnMealCatalog(t.Context(), owner, f.Event.ID, 0)
	require.NoError(t, err)
	day := &catalog.Document.Days[0]
	day.Dinner.Items = nil
	ids := []string{}
	for n := range 40 {
		id := fmt.Sprintf("dish_%02d", n)
		ids = append(ids, id)
		day.Dinner.Items = append(day.Dinner.Items, orders.MealItem{ID: id, PriceMinor: 100,
			Name: orders.Label{
				EN: fmt.Sprintf("Dish %02d ``` **@literal** ", n) + strings.Repeat("😀", 40),
				RU: fmt.Sprintf("Блюдо %02d ``` **@буквально** ", n) + strings.Repeat("😀", 40),
			}})
	}
	published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
	change := completeMeal(db.MealDraftChange{OperationID: uuid.New(), EventID: args.EventID,
		ExpectedEventVersion: args.ExpectedEventVersion, ExpectedCatalogVersion: published.Version,
		ExpectedDraftVersion: args.ExpectedDraftVersion})
	change.Choices[0].DinnerIDs = ids
	draft, err := f.H.store.SaveOwnMealDraft(t.Context(), owner, change)
	require.NoError(t, err)
	args.ExpectedCatalogVersion, args.ExpectedDraftVersion = published.Version, draft.Version
	profile, err := f.H.store.GetProfile(t.Context(), owner)
	require.NoError(t, err)
	_, err = f.H.store.MutateProfile(t.Context(), owner, db.ProfileMutation{OperationID: uuid.New(),
		ExpectedVersion: profile.Version, Kind: "set_language", BotLanguage: locale})
	require.NoError(t, err)
	op := uuid.New()
	wire := prepareMealWire(t, f, op, args)
	receipt, err := f.H.store.OwnMealOrderOperation(t.Context(), owner, args.EventID, op, db.OrderPrepare)
	require.NoError(t, err)
	var prepared db.OrderPreparation
	require.NoError(t, json.Unmarshal(receipt.Result, &prepared))
	require.Equal(t, wire.Preconditions.ProposalID, prepared.ID)
	// The displayed consent must retain all forty dishes even after a newer edit.
	change.OperationID = uuid.New()
	change.ExpectedDraftVersion = draft.Version
	change.Choices[0].DinnerIDs = nil
	_, err = f.H.store.SaveOwnMealDraft(t.Context(), owner, change)
	require.NoError(t, err)
	return f, p, op, prepared
}

func mealApprovalContent(t *testing.T, sent []telegram.SendMessageRequest) string {
	t.Helper()
	var body strings.Builder
	for i, message := range sent {
		require.LessOrEqual(t, len(utf16.Encode([]rune(message.Text))), 4096)
		body.WriteString(message.Text)
		if i < len(sent)-1 {
			require.Nil(t, message.ReplyMarkup, "Every composition chunk must precede the consent keyboard")
		}
		for _, entity := range message.Entities {
			require.NotEqual(t, "text_mention", entity.Type)
		}
	}
	return body.String()
}

func TestMealOrderApprovalShowsFrozenCompositionBeforeLastChunkConsent(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			f, p, op, prepared := mealApprovalPreparation(t, locale)
			raw, err := json.Marshal(prepared.Arguments)
			require.NoError(t, err)
			p.mu.Lock()
			p.approval = &loom.Approval{ID: op.String(), OperationID: op.String(), Method: "zinusya_meals.submit",
				ReplyMode: loom.ReplyModeAgent, Text: "INCOMPLETE TOOL SUMMARY", Arguments: raw,
				ArgumentHash: "exact-meal-hash", ExpiresAt: prepared.ExpiresAt}
			p.mu.Unlock()
			id := acceptOrderApprovalMessage(t, f)
			f.H.await(t, id, "awaiting_user")
			var sent []telegram.SendMessageRequest
			require.Eventually(t, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				sent = append([]telegram.SendMessageRequest(nil), p.sent...)
				return len(sent) > 1 && sent[len(sent)-1].ReplyMarkup != nil
			}, 5*time.Second, 10*time.Millisecond)
			text := mealApprovalContent(t, sent)
			for n := range 40 {
				label := fmt.Sprintf("Dish %02d", n)
				if locale == "ru" {
					label = fmt.Sprintf("Блюдо %02d", n)
				}
				require.Contains(t, text, label)
			}
			for _, part := range []string{"6.65 BYN", "1.00 BYN", "46.65 BYN", prepared.Snapshot.Quote.Timezone} {
				require.Contains(t, text, part)
			}
			require.NotContains(t, text, "INCOMPLETE TOOL SUMMARY")
			var count int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&count),
			)
			require.Zero(t, count, "Presentation never creates an order or consumes the newer draft")
		})
	}
}
