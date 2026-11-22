package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/passcopy"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func passQAUpdate(t *testing.T, h *harness, update telegram.Update) uuid.UUID {
	t.Helper()
	body, err := json.Marshal(update)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		h.server.URL+"/telegram/webhook",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	response, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	var accepted struct {
		ID uuid.UUID `json:"task_id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&accepted))
	require.NotEqual(t, uuid.Nil, accepted.ID)
	return accepted.ID
}

func passQAMessage(t *testing.T, h *harness, updateID, userID int64, text string) uuid.UUID {
	t.Helper()
	return passQAUpdate(t, h, telegram.Update{ID: updateID, Message: &telegram.Message{
		ID: updateID, From: &telegram.User{ID: userID, LanguageCode: "en"},
		Chat: telegram.Chat{ID: userID, Type: "private"}, Text: text,
	}})
}

type passQAReply struct {
	telegram.SendMessageRequest

	messageID int64
}

func passQAReplies(t *testing.T, h *harness, taskID uuid.UUID, userID int64) []passQAReply {
	t.Helper()
	h.await(t, taskID, "completed")
	deliveredNamePrompt(t, h, taskID)
	out, err := h.store.ListOutbox(t.Context(), taskID)
	require.NoError(t, err)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	result := make([]passQAReply, 0, len(out))
	for _, row := range out {
		var receipt struct {
			MessageID int64 `json:"message_id"`
		}
		require.NoError(t, json.Unmarshal(row.Receipt, &receipt))
		require.Positive(t, receipt.MessageID)
		require.LessOrEqual(t, receipt.MessageID, int64(len(h.peers.sent)))
		message := h.peers.sent[receipt.MessageID-1]
		require.Equal(t, userID, message.ChatID)
		result = append(result, passQAReply{SendMessageRequest: message, messageID: receipt.MessageID})
	}
	require.NotEmpty(t, result)
	return result
}

func passQAClick(t *testing.T, h *harness, updateID, userID int64, replies []passQAReply, label string) uuid.UUID {
	t.Helper()
	for _, reply := range replies {
		if keyboard, ok := reply.ReplyMarkup.(*telegram.InlineKeyboardMarkup); ok {
			for _, row := range keyboard.InlineKeyboard {
				for _, button := range row {
					if button.Text != label {
						continue
					}
					require.NotEmpty(t, button.CallbackData)
					return passQAUpdate(t, h, telegram.Update{ID: updateID, CallbackQuery: &telegram.CallbackQuery{
						ID: uuid.NewString(), From: telegram.User{ID: userID, LanguageCode: "en"},
						Message: &telegram.Message{ID: reply.messageID, Chat: telegram.Chat{ID: userID, Type: "private"}},
						Data:    button.CallbackData,
					}})
				}
			}
		}
	}
	t.Fatalf("Missing button %q in %+v", label, replies)
	return uuid.Nil
}

func passQAInviteDraft(t *testing.T, replies []passQAReply, event, code string) string {
	t.Helper()
	for _, reply := range replies {
		for word := range strings.FieldsSeq(reply.Text) {
			if !strings.HasPrefix(word, "https://t.me/") {
				continue
			}
			u, err := url.Parse(word)
			require.NoError(t, err)
			draft := u.Query().Get("text")
			require.Equal(t, "t.me", u.Host)
			require.Equal(t, "/zinusya_test_bot", u.Path)
			require.Equal(t, "/start /passes "+event+" --invite "+code, draft)
			require.Empty(t, u.Query().Get("start"))
			return draft
		}
	}
	t.Fatalf("No pair invitation draft link in %+v", replies)
	return ""
}

func passQAEvent(t *testing.T, h *harness, scope db.PaymentScope, slug string) events.Event {
	t.Helper()
	closeAt := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	event, err := h.store.ChangeEvent(t.Context(), scope.EventScope, scope.Subject, "create-"+slug, db.EventChange{
		ID: uuid.New(),
		Document: events.Document{
			Slug: slug, Name: slug, Timezone: "UTC", StartsAt: closeAt.Add(time.Hour), EndsAt: closeAt.Add(24 * time.Hour),
			Modules: []string{"registrations"},
			Registration: &registrations.Rules{
				LeaderCapacity: 1, FollowerCapacity: 1, PriceMinor: 12345, Currency: "BYN", CurrencyScale: 2,
				ClosesAt: closeAt, PaymentWindowSeconds: 7200,
			},
		},
	})
	require.NoError(t, err)
	return event
}

func passQANamedUser(t *testing.T, h *harness, telegramID int64) db.PaymentScope {
	t.Helper()
	id, _ := registrationUser(t, h, telegramID, false)
	profile := ownerProfileScope(t, h, id)
	changeProfile(t, h, profile, personName(0, "Pair", "Participant"))
	pref := profileChange("set_language", 1)
	pref.BotLanguage = "en"
	changeProfile(t, h, profile, pref)
	return paymentScope(t, h, id, false)
}

func passQARegistration(t *testing.T, h *harness, scope db.PaymentScope, event uuid.UUID) registrations.Registration {
	t.Helper()
	entry, err := h.store.Registration(t.Context(), scope.EventScope, event, scope.UserID)
	require.NoError(t, err)
	return entry
}

func TestPassPairClaimantCancelsAfterReopeningEvent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	peers := newPeers(t)
	peers.loomDown = true
	peers.registrationDetails = detailsProtector(t, 74)
	h := setup(t, peers)
	adminID, _ := registrationUser(t, h, 20140, true)
	admin := paymentScope(t, h, adminID, true)
	sale := passQAEvent(t, h, admin, "cancel-claim")
	sale.Document.Registration.DetailsRequirements = &registrationdetails.Requirements{LegalName: true}
	sale, err := h.store.ChangeEvent(ctx, admin.EventScope, admin.Subject, "require-name", db.EventChange{
		ID: sale.ID, ExpectedVersion: sale.Version, Document: sale.Document,
	})
	require.NoError(t, err)
	const issuerID, claimantID = int64(20141), int64(20142)
	issuer := passQANamedUser(t, h, issuerID)
	claimant := passQANamedUser(t, h, claimantID)
	issue := passQAMessage(t, h, 88400, issuerID, `/passes cancel-claim --role leader --pair --legal-name "First Leader"`)
	passQAReplies(t, h, issue, issuerID)
	invite, err := h.store.ActivePassInvite(ctx, issuer, sale.ID)
	require.NoError(t, err)
	claim := passQAMessage(t, h, 88401, claimantID, "/passes cancel-claim --invite "+invite.Code)
	first := passQAReplies(t, h, claim, claimantID)
	revoke := passcopy.Text("en", "entry.revoke_invite")
	keyboard, ok := first[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Contains(t, keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1][0].Text, revoke)
	require.NotContains(t, first[0].Text, invite.Code)
	opened := passQAMessage(t, h, 88402, claimantID, "/passes cancel-claim")
	replies := passQAReplies(t, h, opened, claimantID)
	require.Contains(
		t,
		replies[0].Text,
		passcopy.Text("en", "entry.invite_claimed", passcopy.Text("en", "entry.follower")),
	)
	require.NotContains(t, replies[0].Text, invite.Code)
	canceled := passQAClick(t, h, 88403, claimantID, replies, revoke)
	require.Contains(t, passQAReplies(t, h, canceled, claimantID)[0].Text, passcopy.Text("en", "entry.invite_revoked"))
	_, err = h.store.ActivePassInvite(ctx, issuer, sale.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.Registration(ctx, claimant.EventScope, sale.ID, claimant.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	entry := passQARegistration(t, h, issuer, sale.ID)
	_, err = h.store.PrepareAssignment(ctx, admin.EventScope, admin.Subject, "allocate-solo", db.PassAssignment{
		EventID: sale.ID, RegistrationID: entry.ID, ExpectedEventVersion: sale.Version, ExpectedVersion: entry.Version,
	})
	require.NoError(t, err)
}

func TestPassPairTelegramDraftWaitlistAndUncouple(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.registrationDetails = detailsProtector(t, 74)
	h := setup(t, p)
	adminID, _ := registrationUser(t, h, 20100, true)
	admin := paymentScope(t, h, adminID, true)
	sale := passQAEvent(t, h, admin, "pair-sale")
	other := passQAEvent(t, h, admin, "other-sale")
	const issuerID, claimantID, competitorID = int64(20101), int64(20102), int64(20103)
	issuer := passQANamedUser(t, h, issuerID)
	claimant := passQANamedUser(t, h, claimantID)
	competitor := passQANamedUser(t, h, competitorID)
	const laterLeaderID = int64(20104)
	laterLeader := passQANamedUser(t, h, laterLeaderID)

	solo := passQAMessage(
		t,
		h,
		88100,
		claimantID,
		`/passes pair-sale --role follower --solo --legal-name "First Follower"`,
	)
	passQAReplies(t, h, solo, claimantID)
	followerBefore := passQARegistration(t, h, claimant, sale.ID)
	laterIssue := passQAMessage(
		t,
		h,
		88101,
		laterLeaderID,
		`/passes pair-sale --role leader --pair --legal-name "Second Leader"`,
	)
	passQAReplies(t, h, laterIssue, laterLeaderID)
	laterInvite, err := h.store.ActivePassInvite(t.Context(), laterLeader, sale.ID)
	require.NoError(t, err)
	issue := passQAMessage(t, h, 88102, issuerID, `/passes pair-sale --role leader --pair --legal-name "First Leader"`)
	issued := passQAReplies(t, h, issue, issuerID)
	invite, err := h.store.ActivePassInvite(t.Context(), issuer, sale.ID)
	require.NoError(t, err)
	draft := passQAInviteDraft(t, issued, sale.Document.Slug, invite.Code)
	leaderBefore := passQARegistration(t, h, issuer, sale.ID)
	require.True(t, followerBefore.RequestedAt.Before(leaderBefore.RequestedAt))

	mismatched := passQAMessage(t, h, 88103, competitorID, strings.Replace(draft, "pair-sale", "other-sale", 1))
	passQAReplies(t, h, mismatched, competitorID)
	for _, event := range []events.Event{sale, other} {
		_, lookupErr := h.store.Registration(t.Context(), competitor.EventScope, event.ID, competitor.UserID)
		require.ErrorIs(t, lookupErr, pgx.ErrNoRows)
	}

	claim := passQAMessage(t, h, 88104, claimantID, draft)
	claimed := passQAReplies(t, h, claim, claimantID)
	require.Contains(t, claimed[0].Text, passcopy.Text("en", "entry.invite_paired"))
	require.Equal(t, claim, passQAMessage(t, h, 88104, claimantID, draft))
	losingClaim := passQAMessage(t, h, 88105, competitorID, "/passes "+invite.Code)
	passQAReplies(t, h, losingClaim, competitorID)
	_, err = h.store.Registration(t.Context(), competitor.EventScope, sale.ID, competitor.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	var pairID, winner uuid.UUID
	var queued time.Time
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), `
SELECT i.claimant_user_id, p.id, p.queued_at
FROM shuttle_pass_invites i JOIN shuttle_pairs p ON p.id = i.pair_id
WHERE i.code = $1 AND p.state = 'waitlisted'`, invite.Code).Scan(&winner, &pairID, &queued))
	require.Equal(t, claimant.UserID, winner)
	require.True(t, followerBefore.RequestedAt.Equal(queued))

	leader := passQARegistration(t, h, issuer, sale.ID)
	follower := passQARegistration(t, h, claimant, sale.ID)
	require.Equal(t, leaderBefore.RequestedAt, leader.RequestedAt)
	require.Equal(t, followerBefore.RequestedAt, follower.RequestedAt)
	require.Nil(t, leader.Pass)
	require.Nil(t, follower.Pass)

	laterClaim := passQAMessage(t, h, 88106, competitorID, `/passes `+laterInvite.Code+` --legal-name "Second Follower"`)
	passQAReplies(t, h, laterClaim, competitorID)
	laterLeaderBefore := passQARegistration(t, h, laterLeader, sale.ID)
	laterFollowerBefore := passQARegistration(t, h, competitor, sale.ID)
	require.True(t, followerBefore.RequestedAt.Before(laterLeaderBefore.RequestedAt))
	require.True(t, laterLeaderBefore.RequestedAt.Before(leaderBefore.RequestedAt))
	require.True(t, leaderBefore.RequestedAt.Before(laterFollowerBefore.RequestedAt))
	for index, fixture := range []struct {
		telegramID int64
		want       db.PassQueuePosition
	}{
		{claimantID, db.PassQueuePosition{Individual: 1, Role: 1, Pair: 1}},
		{laterLeaderID, db.PassQueuePosition{Individual: 2, Role: 1, Pair: 2}},
		{issuerID, db.PassQueuePosition{Individual: 3, Role: 2, Pair: 1}},
		{competitorID, db.PassQueuePosition{Individual: 4, Role: 2, Pair: 2}},
	} {
		menu := passQAMessage(t, h, 88110+int64(index), fixture.telegramID, "/passes pair-sale")
		replies := passQAReplies(t, h, menu, fixture.telegramID)
		require.Contains(t, replies[0].Text, passcopy.Text("en", "entry.individual_position", fixture.want.Individual))
		require.Contains(t, replies[0].Text, passcopy.Text("en", "entry.role_position", fixture.want.Role))
		require.Contains(t, replies[0].Text, passcopy.Text("en", "entry.pair_position", fixture.want.Pair))
	}

	sale.Document.Registration.AutoAssign = true
	_, err = h.store.ChangeEvent(t.Context(), admin.EventScope, admin.Subject, "enable-pair-promotion", db.EventChange{
		ID: sale.ID, ExpectedVersion: sale.Version, Document: sale.Document,
	})
	require.NoError(t, err)
	leaderNoticeText, _, _ := strings.Cut(passcopy.Text("en", "queue.assigned", sale.Document.Name, "Leader"), "\n")
	followerNoticeText, _, _ := strings.Cut(passcopy.Text("en", "queue.assigned", sale.Document.Name, "Follower"), "\n")
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		leaderNotice, followerNotice := false, false
		for _, sent := range p.sent {
			leaderNotice = leaderNotice || sent.ChatID == issuerID && strings.Contains(sent.Text, leaderNoticeText)
			followerNotice = followerNotice || sent.ChatID == claimantID && strings.Contains(sent.Text, followerNoticeText)
		}
		return leaderNotice && followerNotice
	}, 15*time.Second, 25*time.Millisecond, "The queue worker must offer the earlier pair two owner-specific passes")
	leaderAssigned := passQARegistration(t, h, issuer, sale.ID)
	followerAssigned := passQARegistration(t, h, claimant, sale.ID)
	require.NotNil(t, leaderAssigned.Pass)
	require.NotNil(t, followerAssigned.Pass)
	require.NotEqual(t, leaderAssigned.Pass.ID, followerAssigned.Pass.ID)
	require.Equal(t, laterLeaderBefore, passQARegistration(t, h, laterLeader, sale.ID))
	require.Equal(t, laterFollowerBefore, passQARegistration(t, h, competitor, sale.ID))

	_, err = h.store.PaymentBalance(t.Context(), issuer, sale.ID, followerAssigned.Pass.ID)
	require.ErrorIs(t, err, payments.ErrDenied)
	_, err = h.store.PaymentBalance(t.Context(), claimant, sale.ID, leaderAssigned.Pass.ID)
	require.ErrorIs(t, err, payments.ErrDenied)

	menu := passQAMessage(t, h, 88114, issuerID, "/passes pair-sale")
	label := passcopy.Text("en", "entry.leave_pair")
	leave := passQAClick(t, h, 88115, issuerID, passQAReplies(t, h, menu, issuerID), label)
	confirmation := passQAReplies(t, h, leave, issuerID)
	require.Contains(t, confirmation[0].Text, passcopy.Text("en", "entry.leave_confirm"))
	var state string
	require.NoError(
		t,
		h.store.Pool().QueryRow(t.Context(), "SELECT state FROM shuttle_pairs WHERE id=$1", pairID).Scan(&state),
	)
	require.Equal(t, "assigned", state)
	confirmed := passQAClick(t, h, 88116, issuerID, confirmation, label)
	passQAReplies(t, h, confirmed, issuerID)
	require.NoError(
		t,
		h.store.Pool().QueryRow(t.Context(), "SELECT state FROM shuttle_pairs WHERE id=$1", pairID).Scan(&state),
	)
	require.Equal(t, "dissolved", state)
	leader = passQARegistration(t, h, issuer, sale.ID)
	follower = passQARegistration(t, h, claimant, sale.ID)
	require.Equal(t, leaderBefore.RequestedAt, leader.RequestedAt)
	require.Equal(t, followerBefore.RequestedAt, follower.RequestedAt)
	require.Equal(t, leaderAssigned.Pass, leader.Pass)
	require.Equal(t, followerAssigned.Pass, follower.Pass)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Zero(t, p.loomUnavailableCalls)
}

func TestPassPairTelegramDraftReservesBeforeOnboarding(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	adminID, _ := registrationUser(t, h, 20120, true)
	sale := passQAEvent(t, h, paymentScope(t, h, adminID, true), "onboarding-sale")
	const issuerID, claimantID, competitorID = int64(20121), int64(20122), int64(20123)
	issuer := passQANamedUser(t, h, issuerID)
	competitor := passQANamedUser(t, h, competitorID)
	issue := passQAMessage(t, h, 88200, issuerID, "/passes onboarding-sale --role leader --pair")
	issued := passQAReplies(t, h, issue, issuerID)
	invite, err := h.store.ActivePassInvite(t.Context(), issuer, sale.ID)
	require.NoError(t, err)
	draft := passQAInviteDraft(t, issued, sale.Document.Slug, invite.Code)

	start := passQAMessage(t, h, 88201, claimantID, draft)
	var claimedBy, currentPair *uuid.UUID
	require.NoError(t, h.store.Pool().QueryRow(t.Context(),
		"SELECT claimant_user_id, pair_id FROM shuttle_pass_invites WHERE code=$1", invite.Code).
		Scan(&claimedBy, &currentPair))
	require.NotNil(t, claimedBy)
	require.Nil(t, currentPair)
	_, err = h.store.Registration(t.Context(), issuer.EventScope, sale.ID, *claimedBy)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	losingClaim := passQAMessage(t, h, 88202, competitorID, draft)
	passQAReplies(t, h, losingClaim, competitorID)
	_, err = h.store.Registration(t.Context(), competitor.EventScope, sale.ID, competitor.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	h.await(t, start, "awaiting_user")
	deliveredNamePrompt(t, h, start)
	profile, pending, token := agentRequestsName(t, h, start)
	require.Equal(t, profile.UserID, *claimedBy)
	require.NotNil(t, pending.NameExpectation)

	h.stop(t)
	h.start(t)
	code, raw := profileRequest(t, h, token, "complete_name", uuid.New(), map[string]any{
		"expected_version": pending.Version, "expectation_id": pending.NameExpectation.ID,
		"first_name": "Anna", "last_name": "Silva",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	p.mu.Lock()
	p.question = false
	p.mu.Unlock()
	answer := passQAMessage(t, h, 88203, claimantID, "Anna Silva")
	h.await(t, answer, "completed")
	h.await(t, start, "completed")
	childID := uuid.NewSHA1(start, []byte("start-continuation"))
	child := passQAReplies(t, h, childID, claimantID)
	require.Contains(t, child[0].Text, passcopy.Text("en", "entry.invite_paired"))

	claimant := paymentScope(t, h, profile.UserID, false)
	follower := passQARegistration(t, h, claimant, sale.ID)
	require.Equal(t, registrations.Follower, follower.Role)
	require.Equal(t, registrations.Waitlisted, follower.State)
	var pairID uuid.UUID
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), `
SELECT p.id FROM shuttle_pass_invites i JOIN shuttle_pairs p ON p.id=i.pair_id
WHERE i.code=$1 AND i.claimant_user_id=$2 AND p.follower_registration_id=$3`,
		invite.Code, claimant.UserID, follower.ID).Scan(&pairID))
	require.NotEqual(t, uuid.Nil, pairID)
	require.Equal(t, start, passQAMessage(t, h, 88201, claimantID, draft))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1, "Onboarding uses Loom once and registration continues natively")
}
