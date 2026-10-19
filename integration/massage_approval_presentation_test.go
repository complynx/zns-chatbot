package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestMassageApprovalKeepsCanonicalQuoteAndInputBindingWithoutJSON(t *testing.T) {
	t.Parallel()
	cases := []massageApprovalCase{
		{"en", "book", false},
		{"ru", "book", true},
		{"ru", "cancel", false},
		{"en", "cancel", true},
	}
	for _, tc := range cases {
		for _, quoted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/reply=%t", tc.language, tc.kind, quoted), func(t *testing.T) {
				t.Parallel()
				checkMassageApprovalPresentation(t, tc, quoted)
			})
		}
	}
}

type massageApprovalCase struct {
	language string
	kind     string
	adopted  bool
}

func checkMassageApprovalPresentation(t *testing.T, tc massageApprovalCase, quoted bool) {
	t.Helper()

	p := newPeers(t)
	f, owner, _, schedule := massageBookingFixtureWithPeers(t, 2, p)
	profile, err := f.H.store.GetProfile(t.Context(), owner)
	require.NoError(t, err)
	changeProfile(t, f.H, owner, db.ProfileMutation{
		OperationID: uuid.New(), Kind: "set_language",
		ExpectedVersion: profile.Version, BotLanguage: tc.language,
	})
	args := massageBookingArgs(f, schedule, 0)
	var arguments any = args
	var current db.MassageBooking
	var cancel db.MassageCancellationArguments
	if tc.kind == "cancel" {
		current, _ = bookMassage(t, f, owner, args)
		cancel = db.MassageCancellationArguments{
			EventID: f.Event.ID, BookingID: current.ID, ExpectedVersion: current.Version,
		}
		arguments = cancel
	}
	operation := uuid.New()
	var canonical uuid.UUID
	var originalExpiry time.Time
	if tc.adopted {
		session := nativeMassageSession(t, f, owner, 11002)
		deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
		nativeOperation := uuid.New()
		canonical, originalExpiry = prepareNativeMassageQuote(
			t,
			f,
			deployment,
			session,
			nativeOperation,
			tc.kind,
			args,
			cancel,
		)
		raw, e := json.Marshal(arguments)
		require.NoError(t, e)
		var adopted map[string]any
		require.NoError(t, json.Unmarshal(raw, &adopted))
		adopted["existing_proposal_id"] = canonical
		arguments = adopted
	}
	f.H.start(t)
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost,
		massageOwnerAPI+tc.kind+"/prepare", map[string]any{
			"protocol_version": 1, "operation_id": operation, "arguments": arguments,
		})
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared struct {
		Description   string          `json:"description"`
		Arguments     json.RawMessage `json:"arguments"`
		ExpiresAt     time.Time       `json:"expires_at"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.NotEmpty(t, prepared.Description)
	require.LessOrEqual(t, len(prepared.Description), 4000)
	if tc.adopted {
		require.Equal(t, canonical, prepared.Preconditions.ProposalID)
		require.Equal(t, originalExpiry, prepared.ExpiresAt)
	}
	// Loom can shorten consent lifetime; presentation must not replace it with proposal expiry.
	expiry := prepared.ExpiresAt.Add(-30 * time.Second)
	p.mu.Lock()
	p.approval = &loom.Approval{
		ID: operation.String(), OperationID: operation.String(),
		Method: "zinusya_massage." + tc.kind, ReplyMode: loom.ReplyModeAgent,
		Text: prepared.Description, ConfirmationText: "MODEL TEXT MUST NOT REPLACE THE QUOTE",
		Arguments: prepared.Arguments, ArgumentHash: "fixture-exact-binding", ExpiresAt: expiry,
	}
	p.mu.Unlock()
	first := acceptMassageContextText(t, f.H, 97600, "Continue the exact massage choice")
	f.H.await(t, first, "awaiting_user")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	sent := p.sent[0]
	p.mu.Unlock()
	require.NotContains(t, sent.Text, "event_id")
	require.NotContains(t, sent.Text, "expected_version")
	require.NotContains(t, sent.Text, "expected_schedule_version")
	require.NotContains(t, sent.Text, "existing_proposal_id")
	require.NotContains(t, sent.Text, "MODEL TEXT")
	require.Contains(t, sent.Text, "45.00 BYN")
	zone, err := time.LoadLocation(f.Event.Document.Timezone)
	require.NoError(t, err)
	require.Contains(t, sent.Text, args.Selection.StartsAt.In(zone).Format(time.RFC3339))
	require.Contains(t, sent.Text, f.Event.Document.Timezone)
	if tc.language == "ru" {
		require.Contains(t, sent.Text, "Специалист")
	} else {
		require.Contains(t, sent.Text, "Specialist")
	}
	require.IsType(t, &telegram.ReplyKeyboardMarkup{}, sent.ReplyMarkup)
	f.H.stop(t)
	f.H.start(t)
	second := acceptMassageApprovalReply(t, f.H, first, quoted)
	f.H.await(t, second, "completed")
	f.H.await(t, first, "completed")
	f.H.stop(t)
	p.mu.Lock()
	inputs := append([]loom.InputRequest(nil), p.inputs...)
	p.mu.Unlock()
	require.Len(t, inputs, 1)
	require.Equal(t, "steering", inputs[0].Kind)
	require.Empty(t, inputs[0].Decision)
	if quoted {
		require.NotNil(t, inputs[0].Presentation)
		require.Equal(t, operation.String(), inputs[0].Presentation.ApprovalID)
		require.NotEmpty(t, inputs[0].Presentation.ReplyTo)
	} else {
		require.Nil(t, inputs[0].Presentation)
	}
	p.mu.Lock()
	require.Len(t, p.runs, 1, "Plain text and quoted replies continue the existing dialogue")
	p.mu.Unlock()
	require.True(t, strings.HasPrefix(inputs[0].Text, "Do not execute; I want to clarify the time"))
	bookings, err := f.H.store.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	if tc.kind == "book" {
		require.Empty(t, bookings.Bookings)
	} else {
		require.Len(t, bookings.Bookings, 1)
		require.Equal(t, current.ID, bookings.Bookings[0].ID)
		require.Equal(t, "booked", bookings.Bookings[0].State)
	}
}

func prepareNativeMassageQuote(
	t *testing.T, f paymentFixture, deployment db.EventScope, session db.MiniAppMassageSession,
	operation uuid.UUID, kind string, book db.MassageBookingArguments, cancel db.MassageCancellationArguments,
) (uuid.UUID, time.Time) {
	t.Helper()
	if kind == "book" {
		prepared, err := f.H.store.PrepareMiniAppMassageBooking(t.Context(), deployment, session, operation, book)
		require.NoError(t, err)
		return prepared.ID, prepared.ExpiresAt
	}
	prepared, err := f.H.store.PrepareMiniAppMassageCancellation(t.Context(), deployment, session, operation, cancel)
	require.NoError(t, err)
	return prepared.ID, prepared.ExpiresAt
}

func acceptMassageApprovalReply(t *testing.T, h *harness, parent uuid.UUID, quoted bool) uuid.UUID {
	t.Helper()
	update := telegram.Update{ID: 97601, Message: &telegram.Message{
		ID: 97601, From: &telegram.User{ID: 11002}, Chat: telegram.Chat{ID: 11002, Type: "private"},
		Text: "Do not execute; I want to clarify the time",
	}}
	if quoted {
		rows, err := h.store.ListOutbox(t.Context(), parent)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		var receipt struct {
			MessageID int64 `json:"message_id"`
		}
		require.NoError(t, json.Unmarshal(rows[0].Receipt, &receipt))
		require.Positive(t, receipt.MessageID)
		update.Message.ReplyToMessage = &telegram.Message{ID: receipt.MessageID}
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	return accepted.ID
}
