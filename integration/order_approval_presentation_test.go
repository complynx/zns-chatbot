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

func orderApprovalFixture(t *testing.T, p *peers, large bool) (paymentFixture, db.ProfileScope, db.OrderSubmission) {
	t.Helper()
	f, scope, draft := selectionFixtureWithPeers(t, p)
	doc := catalogDocument()
	doc.CompleteCapacity = &orders.CompleteCapacity{
		Version: 1,
		Resources: []orders.CapacityResource{
			{
				ID:      "kitchen",
				Name:    orders.Label{EN: "Kitchen ``` **literal**", RU: "Кухня ``` **буквально**"},
				Ceiling: 0,
			},
		},
		Bindings: []orders.CapacityBinding{{OfferID: "lunch", ResourceID: "kitchen", Units: 3}},
	}
	doc.CompletePricing = &orders.CompletePricing{
		Version: 1,
		Supplements: []orders.Supplement{
			{
				ID:         "service",
				Name:       orders.Label{EN: "Service ``` **literal**", RU: "Сбор ``` **буквально**"},
				PriceMinor: 100,
				Mode:       orders.PerOffer,
			},
		},
	}
	doc.Items[0].RequiredSupplementIDs = []string{"service"}
	if large {
		doc.Items = nil
		doc.Offers[0].ItemIDs = nil
		draft.Selections = nil
		for n := range 40 {
			id := fmt.Sprintf("item_%02d", n)
			doc.Items = append(
				doc.Items,
				orders.Item{
					ID: id,
					Name: orders.Label{
						EN: strings.Repeat("literal😀 ", 10),
						RU: strings.Repeat("буквально😀 ", 8),
					},
					BasePriceMinor:        1234,
					RequiredSupplementIDs: []string{"service"},
				},
			)
			doc.Offers[0].ItemIDs = append(doc.Offers[0].ItemIDs, id)
			draft.Selections = append(draft.Selections, orders.Selection{OfferID: "lunch", ItemID: id, Quantity: 2})
		}
	} else {
		draft.Selections = []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}}
	}
	change := db.OrdersCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      1,
		Document:             doc,
	}
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	draft.ExpectedCatalogVersion = 2
	saved, err := f.H.store.SaveOwnOrdersSelection(t.Context(), scope, draft)
	require.NoError(t, err)
	return f, scope, db.OrderSubmission{
		EventID:                draft.EventID,
		ExpectedEventVersion:   draft.ExpectedEventVersion,
		ExpectedCatalogVersion: 2,
		ExpectedDraftVersion:   saved.Version,
	}
}

func acceptOrderApprovalMessage(t *testing.T, f paymentFixture) uuid.UUID {
	t.Helper()
	update := telegram.Update{
		ID: 99371,
		Message: &telegram.Message{
			ID:   99371,
			From: &telegram.User{ID: 11002},
			Chat: telegram.Chat{ID: 11002, Type: "private"},
			Text: "Please submit my exact order",
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	task, _, err := f.H.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	return task.ID
}

func TestOrderApprovalShowsCompleteImmutableQuoteBeforeLastChunkKeyboard(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			f, scope, args := orderApprovalFixture(t, p, true)
			language := locale
			_, err := f.H.store.MutateProfile(
				t.Context(),
				scope,
				db.ProfileMutation{
					OperationID:     uuid.New(),
					ExpectedVersion: 0,
					Kind:            "set_language",
					BotLanguage:     language,
				},
			)
			require.NoError(t, err)
			operation := uuid.New()
			prepared, err := f.H.store.PrepareOwnOrderSubmission(
				t.Context(),
				scope,
				operation,
				args,
				db.OrderSourceAgent,
			)
			require.NoError(t, err)
			// Change the current draft after preparation. Presentation must retain the
			// proposed quote rather than quietly replacing it with current quantities.
			_, err = f.H.store.SaveOwnOrdersSelection(
				t.Context(),
				scope,
				db.OrdersSelectionChange{
					OperationID:            uuid.New(),
					EventID:                args.EventID,
					ExpectedEventVersion:   args.ExpectedEventVersion,
					ExpectedCatalogVersion: args.ExpectedCatalogVersion,
					ExpectedDraftVersion:   args.ExpectedDraftVersion,
					Selections:             []orders.Selection{{OfferID: "lunch", ItemID: "item_00", Quantity: 9}},
				},
			)
			require.NoError(t, err)
			raw, err := json.Marshal(args)
			require.NoError(t, err)
			p.mu.Lock()
			p.approval = &loom.Approval{
				ID:           operation.String(),
				OperationID:  operation.String(),
				Method:       "zinusya_orders.submit",
				ReplyMode:    loom.ReplyModeAgent,
				Text:         "UNTRUSTED INCOMPLETE SUMMARY",
				Arguments:    raw,
				ArgumentHash: "exact-hash",
				ExpiresAt:    prepared.ExpiresAt,
			}
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
			var combined strings.Builder
			for n, message := range sent {
				require.LessOrEqual(t, len(utf16.Encode([]rune(message.Text))), 4096)
				combined.WriteString(message.Text)
				if n < len(sent)-1 {
					require.Nil(t, message.ReplyMarkup, "All quote chunks must precede consent buttons")
				}
				for _, entity := range message.Entities {
					require.NotEqual(t, "text_mention", entity.Type)
				}
			}
			text := combined.String()
			require.NotContains(t, text, "UNTRUSTED INCOMPLETE SUMMARY")
			require.NotContains(t, text, "expected_draft_version")
			for n := range 40 {
				require.Contains(t, text, fmt.Sprintf("lunch/item_%02d", n))
			}
			require.Contains(t, text, "12.34 BYN")
			require.Contains(t, text, "24.68 BYN")
			require.Contains(t, text, "1.00 BYN")
			require.Contains(t, text, "988.20 BYN")
			require.Contains(t, text, "(kitchen): 3")
			require.Contains(t, text, map[string]string{"en": "Kitchen", "ru": "Кухня"}[locale])
			require.Contains(t, text, prepared.Snapshot.Quote.Timezone)
			require.Len(t, prepared.Snapshot.Services, 1)
			zone, err := time.LoadLocation(prepared.Snapshot.Quote.Timezone)
			require.NoError(t, err)
			service := prepared.Snapshot.Services[0]
			require.Contains(t, text, service.StartsAt.In(zone).Format(time.RFC3339))
			require.Contains(t, text, service.EndsAt.In(zone).Format(time.RFC3339))
			require.Contains(t, text, map[string]string{"en": "Lunch", "ru": "Обед"}[locale])
			if locale == "ru" {
				require.Contains(t, text, "Обязательные доплаты")
				require.Contains(t, text, "Итого")
				require.Contains(t, text, "Сбор")
			} else {
				require.Contains(t, text, "Required charges")
				require.Contains(t, text, "Total")
				require.Contains(t, text, "Service")
			}
			var count int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&count),
			)
			require.Zero(t, count, "Rendering approval creates no order")
		})
	}
}

func TestOrderApprovalWaitsForExactReceiptInsteadOfDeliveringIncompletePrompt(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f, scope, args := orderApprovalFixture(t, p, false)
	operation := uuid.New()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	p.mu.Lock()
	p.approval = &loom.Approval{
		ID:          operation.String(),
		OperationID: operation.String(),
		Method:      "zinusya_orders.submit",
		ReplyMode:   loom.ReplyModeAgent,
		Text:        "INCOMPLETE PROMPT MUST NOT BE SENT",
		Arguments:   raw,
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	p.mu.Unlock()
	id := acceptOrderApprovalMessage(t, f)
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.createAttempts)+p.runGets >= 2 },
		5*time.Second,
		10*time.Millisecond,
	)
	p.mu.Lock()
	require.Empty(t, p.sent, "Missing immutable preparation must not fall back to incomplete approval")
	p.mu.Unlock()
	_, err = f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	f.H.await(t, id, "awaiting_user")
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.sent) > 0 && p.sent[len(p.sent)-1].ReplyMarkup != nil
	}, 5*time.Second, 10*time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	var text strings.Builder
	for _, message := range p.sent {
		text.WriteString(message.Text)
	}
	require.Contains(t, text.String(), "Soup")
	require.Contains(t, text.String(), "25.68 BYN")
	require.NotContains(t, text.String(), "INCOMPLETE PROMPT")
}

func TestOrderApprovalReturnsAfterQuestionWithoutChangingApprovalIdentity(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f, scope, args := orderApprovalFixture(t, p, false)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	p.mu.Lock()
	p.approval = &loom.Approval{
		ID:           operation.String(),
		OperationID:  operation.String(),
		Method:       "zinusya_orders.submit",
		ReplyMode:    loom.ReplyModeAgent,
		Text:         "Bounded summary",
		Arguments:    raw,
		ArgumentHash: "stable-action",
		ExpiresAt:    prepared.ExpiresAt,
	}
	p.mu.Unlock()
	id := acceptOrderApprovalMessage(t, f)
	f.H.await(t, id, "awaiting_user")
	replyChoice(t, p, f.H, id)
	p.mu.Lock()
	for key, run := range p.runs {
		run.Question = &loom.Question{
			ID:        "order-details",
			Text:      "Clarify this order first",
			Choices:   []loom.Choice{{ID: "continue", Label: "Continue"}},
			AllowText: true,
			ExpiresAt: time.Now().Add(time.Minute),
		}
		p.runs[key] = run
	}
	p.mu.Unlock()
	var questionMessages int
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.sent) > 0 && p.sent[len(p.sent)-1].Text == "Clarify this order first" &&
			p.sent[len(p.sent)-1].ReplyMarkup != nil {
			questionMessages = len(p.sent)
			return true
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
	p.mu.Lock()
	for key, run := range p.runs {
		run.Question = nil
		p.runs[key] = run
	}
	p.mu.Unlock()
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.sent) > questionMessages && strings.Contains(p.sent[len(p.sent)-1].Text, "25.68 BYN") &&
			p.sent[len(p.sent)-1].ReplyMarkup != nil
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "Approve", replyChoice(t, p, f.H, id))
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
}

func TestOrderApprovalOffersOriginalNativeOperationWithoutConsumingConsent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{loom.ReplyModeAgent, loom.ReplyModeProtocol} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.miniAppOrigin = appTestOrigin
			f, scope, args := orderApprovalFixture(t, p, false)
			op := uuid.New()
			prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
			require.NoError(t, err)
			raw, err := json.Marshal(args)
			require.NoError(t, err)
			p.mu.Lock()
			p.approval = &loom.Approval{
				ID:           op.String(),
				OperationID:  op.String(),
				Method:       "zinusya_orders.submit",
				ReplyMode:    mode,
				Text:         "Review original order",
				Arguments:    raw,
				ArgumentHash: "exact-hash",
				ExpiresAt:    prepared.ExpiresAt,
			}
			p.mu.Unlock()
			id := acceptOrderApprovalMessage(t, f)
			f.H.await(t, id, "awaiting_user")
			var sent []telegram.SendMessageRequest
			require.Eventually(t, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				sent = append([]telegram.SendMessageRequest(nil), p.sent...)
				return len(sent) >= 2 && sent[len(sent)-1].ReplyMarkup != nil
			}, 5*time.Second, 10*time.Millisecond)
			form, ok := sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
			require.True(t, ok, "Native launch must use inline WebApp to retain Telegram initData")
			require.Len(t, form.InlineKeyboard, 1)
			button := form.InlineKeyboard[0][0]
			require.NotNil(t, button.WebApp)
			require.Equal(
				t,
				appTestOrigin+"/app/#orders?event="+args.EventID.String()+"&operation="+op.String(),
				button.WebApp.URL,
			)
			require.Empty(t, button.CallbackData)
			if mode == loom.ReplyModeAgent {
				_, ok = sent[len(sent)-1].ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
				require.True(t, ok, "Latest dialogue question retains its conversational answer keyboard")
			}
			var orders int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&orders),
			)
			require.Zero(t, orders)
			rows, err := f.H.store.ListOutbox(t.Context(), id)
			require.NoError(t, err)
			var stored telegram.SendMessageRequest
			require.NoError(t, json.Unmarshal(rows[0].Payload, &stored))
			replayed := stored.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
			require.Equal(
				t,
				button.WebApp.URL,
				replayed.InlineKeyboard[0][0].WebApp.URL,
				"Durable replay retains the original operation link",
			)
		})
	}
}
