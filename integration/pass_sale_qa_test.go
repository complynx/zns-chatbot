package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type passQAFixture struct {
	h      *harness
	p      *peers
	scope  db.EventScope
	event  events.Event
	update int64
}

type passQAUser struct {
	telegramID int64
	app        appSession
}

func newPassQAFixture(t *testing.T, tiers []registrations.Tier, requiresDetails bool) *passQAFixture {
	t.Helper()
	p := newPeers(t)
	p.loomDown = true
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 91)
	h := setup(t, p)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rows, err := h.store.Pool().Query(ctx, "SELECT id,state,route,COALESCE(request->>'structured_command','') "+
			"FROM shuttle_tasks ORDER BY created_at DESC LIMIT 8")
		if err != nil {
			t.Logf("Could not read recent tasks: %v", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var state, route, command string
			if scanErr := rows.Scan(&id, &state, &route, &command); scanErr == nil {
				t.Logf("Recent task %s: state=%s route=%s command=%s", id, state, route, command)
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			t.Logf("Could not read all recent tasks: %v", rowsErr)
		}
	})
	f := &passQAFixture{h: h, p: p, scope: db.EventScope{BotID: 123, Organization: "org"}, update: 260000}
	rules := &registrations.Rules{
		LeaderCapacity:       20,
		FollowerCapacity:     20,
		Currency:             "BYN",
		CurrencyScale:        2,
		ClosesAt:             time.Now().UTC().Truncate(time.Microsecond).Add(3 * time.Hour),
		PaymentWindowSeconds: 3600,
		Pricing: &registrations.TierPolicy{
			Mode:  registrations.PricingDistributed,
			Tiers: tiers,
		},
		SalesOpensAt: &tiers[0].StartsAt,
	}
	rules.PassQueueRules = registrations.PassQueueRules{AutoAssign: true, MaxLeaderPercent: 100, MaxFollowerPercent: 100}
	if requiresDetails {
		rules.DetailsRequirements = &registrationdetails.Requirements{PassportNumber: true}
	}
	f.saveEvent(t, events.Document{
		Name: "Synthetic Pass Sale", Slug: "synthetic", Timezone: "Europe/Minsk",
		StartsAt: rules.ClosesAt.Add(time.Hour), EndsAt: rules.ClosesAt.Add(48 * time.Hour),
		Modules: []string{"registrations"}, Registration: rules,
	})
	return f
}

func (f *passQAFixture) saveEvent(t *testing.T, doc events.Document) {
	t.Helper()
	if f.event.ID == uuid.Nil {
		f.event.ID = uuid.New()
	}
	var err error
	f.event, err = f.h.store.ChangeEvent(t.Context(), f.scope, "sale-qa", uuid.NewString(), db.EventChange{
		ID: f.event.ID, ExpectedVersion: f.event.Version, Document: doc,
	})
	require.NoError(t, err)
}

func (f *passQAFixture) user(t *testing.T, id int64) passQAUser {
	t.Helper()
	session := detailsAppSession(t, f.h, id, byte(id%251))
	changeProfile(t, f.h, ownerProfileScope(t, f.h, session.UserID), personName(0, "Synthetic", "Display"))
	return passQAUser{id, session}
}

func (f *passQAFixture) namedUser(t *testing.T, id int64) passQAUser {
	t.Helper()
	user := f.user(t, id)
	body := appDetailsBody(t, uuid.New(), 0, "Synthetic Sale Participant")
	code, raw, _ := appCall(t, f.h, http.MethodPost, appDetailsPath+"/legal-name", body, user.app.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	return user
}

func (f *passQAFixture) accept(
	t *testing.T,
	u passQAUser,
	command string,
	messageID int64,
	data string,
) (uuid.UUID, int) {
	t.Helper()
	f.p.mu.Lock()
	after := len(f.p.sent)
	f.p.mu.Unlock()
	f.update++
	chat := telegram.Chat{ID: u.telegramID, Type: "private"}
	sender := telegram.User{ID: u.telegramID, FirstName: "Synthetic", LanguageCode: "en"}
	update := telegram.Update{ID: f.update}
	if command != "" {
		update.Message = &telegram.Message{ID: f.update, From: &sender, Chat: chat, Text: command}
	} else {
		update.CallbackQuery = &telegram.CallbackQuery{
			ID: uuid.NewString(), From: sender, Message: &telegram.Message{ID: messageID, Chat: chat}, Data: data,
		}
	}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		f.h.server.URL+"/telegram/webhook",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	resp, err := f.h.server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Less(t, resp.StatusCode, 300)
	var receipt struct {
		TaskID uuid.UUID `json:"task_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&receipt))
	require.NotEqual(t, uuid.Nil, receipt.TaskID)
	return receipt.TaskID, after
}

type passQAButton struct {
	messageID int64
	button    telegram.InlineKeyboardButton
}

func (f *passQAFixture) button(t *testing.T, u passQAUser, after int, label string) passQAButton {
	t.Helper()
	var found passQAButton
	require.Eventually(t, func() bool {
		f.p.mu.Lock()
		defer f.p.mu.Unlock()
		for index, sent := range f.p.sent[after:] {
			keyboard, ok := sent.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
			if sent.ChatID != u.telegramID || !ok {
				continue
			}
			for _, row := range keyboard.InlineKeyboard {
				for _, button := range row {
					if button.Text == label {
						found = passQAButton{int64(after + index + 1), button}
						return true
					}
				}
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond, "Telegram chat %d never received button %q", u.telegramID, label)
	return found
}

func (f *passQAFixture) text(t *testing.T, u passQAUser, after int, fragment string) {
	t.Helper()
	require.Eventually(t, func() bool {
		f.p.mu.Lock()
		defer f.p.mu.Unlock()
		for _, sent := range f.p.sent[after:] {
			if sent.ChatID == u.telegramID && strings.Contains(sent.Text, fragment) {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond, "Telegram chat %d never received %q", u.telegramID, fragment)
}

func (f *passQAFixture) click(t *testing.T, u passQAUser, button passQAButton) int {
	t.Helper()
	require.NotEmpty(t, button.button.CallbackData)
	task, after := f.accept(t, u, "", button.messageID, button.button.CallbackData)
	f.h.await(t, task, "completed")
	return after
}

func (f *passQAFixture) menu(t *testing.T, u passQAUser) passQAButton {
	t.Helper()
	task, after := f.accept(t, u, "/passes", 0, "")
	f.h.await(t, task, "completed")
	return f.button(t, u, after, f.event.Document.Name)
}

func (f *passQAFixture) solo(t *testing.T, u passQAUser, after int) int {
	t.Helper()
	after = f.click(t, u, f.button(t, u, after, passcopy.Text("en", "entry.leader")))
	return f.click(t, u, f.button(t, u, after, passcopy.Text("en", "entry.solo")))
}

func (f *passQAFixture) completeDetails(t *testing.T, u passQAUser, after int) {
	t.Helper()
	form := f.button(t, u, after, passcopy.Text("en", "entry.details_form"))
	require.NotNil(t, form.button.WebApp)
	formURL, err := url.Parse(form.button.WebApp.URL)
	require.NoError(t, err)
	request, err := uuid.Parse(formURL.Query().Get("details_request"))
	require.NoError(t, err)
	formURL.RawQuery = ""
	require.Equal(t, appTestOrigin+"/app/", formURL.String())
	profileScope := ownerProfileScope(t, f.h, u.app.UserID)
	expectation, err := f.h.store.DetailsRequest(t.Context(), profileScope, request)
	require.NoError(t, err)
	require.ElementsMatch(
		t,
		[]registrationdetails.Field{registrationdetails.LegalName, registrationdetails.PassportNumber},
		expectation.MissingFields,
	)
	next := f.button(t, u, after, passcopy.Text("en", "entry.details_continue"))
	values := []struct{ field, value string }{
		{"legal-name", "Synthetic Sale Participant"},
		{"passport-number", "SYNTHETIC-SALE-42"},
	}
	for version, value := range values {
		path := appDetailsPath + "/" + value.field
		body := appDetailsBody(t, uuid.New(), int64(version), value.value)
		code, raw, headers := appCall(t, f.h, http.MethodPost, path, body, u.app.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, "no-store", headers.Get("Cache-Control"))
		require.EqualValues(t, version+1, appDetailsStatus(t, raw).Version)
		require.NotContains(t, string(raw), value.value)
	}
	_, err = f.h.store.Registration(t.Context(), f.scope, f.event.ID, u.app.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "saving protected details alone cannot join the queue")
	expectation, err = f.h.store.DetailsRequest(t.Context(), profileScope, request)
	require.NoError(t, err)
	require.Equal(t, "completed", expectation.State)
	f.click(t, u, next)
}

func (f *passQAFixture) registration(t *testing.T, u passQAUser) registrations.Registration {
	t.Helper()
	entry, err := f.h.store.Registration(t.Context(), f.scope, f.event.ID, u.app.UserID)
	require.NoError(t, err)
	return entry
}

func (f *passQAFixture) pass(t *testing.T, u passQAUser, tier string, price int64) registrations.Registration {
	t.Helper()
	var assigned registrations.Registration
	require.Eventually(t, func() bool {
		entry, err := f.h.store.Registration(t.Context(), f.scope, f.event.ID, u.app.UserID)
		assigned = entry
		return err == nil && entry.Pass != nil
	}, 12*time.Second, 25*time.Millisecond, "expected assignment in tier %s for chat %d", tier, u.telegramID)
	require.Equal(t, registrations.Assigned, assigned.State)
	require.NotNil(t, assigned.Pass.Tier)
	require.Equal(t, tier, assigned.Pass.Tier.ID)
	require.Equal(t, price, assigned.Pass.PriceMinor)
	return assigned
}

func (f *passQAFixture) priority(t *testing.T, u passQAUser) (time.Time, time.Time, error) {
	t.Helper()
	var selected, expires time.Time
	err := f.h.store.Pool().QueryRow(t.Context(), "SELECT selected_at, expires_at FROM shuttle_pass_promo_choices "+
		"WHERE event_id=$1 AND user_id=$2", f.event.ID, u.app.UserID).Scan(&selected, &expires)
	return selected, expires, err
}

func (f *passQAFixture) noLoom(t *testing.T) {
	t.Helper()
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Empty(t, f.p.runs)
	require.Zero(t, f.p.loomUnavailableCalls)
}

func passQAPromoTiers(start time.Time) []registrations.Tier {
	return []registrations.Tier{
		{ID: "promo", PriceMinor: 7000, Amount: 1, StartsAt: start, Promo: true},
		{ID: "regular", PriceMinor: 15000, Amount: 10, StartsAt: start.Add(time.Hour), BlockedByDate: true},
	}
}

func TestPassSaleQAOpeningAndProtectedSolo(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	user := f.user(t, 26101)
	after := f.click(t, user, f.menu(t, user))
	location, err := time.LoadLocation(f.event.Document.Timezone)
	require.NoError(t, err)
	localStart := start.In(location).Format("2006-01-02 15:04") + " (" + location.String() + ")"
	f.text(t, user, after, passcopy.Text("en", "entry.opens", f.event.Document.Name, localStart))
	_, _, err = f.priority(t, user)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.h.store.Registration(t.Context(), f.scope, f.event.ID, user.app.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	doc := f.event.Document
	var opening time.Time
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&opening))
	doc.Registration.SalesOpensAt = &opening
	doc.Registration.Pricing.Tiers = passQAPromoTiers(opening)
	f.saveEvent(t, doc)
	after = f.click(t, user, f.menu(t, user))
	selected, expires, err := f.priority(t, user)
	require.NoError(t, err)
	require.False(t, selected.Before(opening))
	require.Equal(t, time.Hour, expires.Sub(selected))
	f.completeDetails(t, user, f.solo(t, user, after))
	pass := f.pass(t, user, "promo", 7000)
	require.Equal(t, registrations.Leader, pass.Role)
	require.Equal(t, user.app.UserID, pass.UserID)
	scope := ownerProfileScope(t, f.h, user.app.UserID)
	profile, err := f.h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, "Synthetic Display", profile.FullName)
	after = f.click(t, user, f.menu(t, user))
	due := pass.Pass.PaymentDueAt.In(location).Format("2006-01-02 15:04 MST")
	f.text(t, user, after, passcopy.Text(
		"en", "queue.assigned", f.event.Document.Name, passcopy.Text("en", "entry.leader"), "70.00 BYN", due, "/payments",
	))
	require.Equal(t, pass, f.registration(t, user))
	f.noLoom(t)
}

func TestPassSaleQAPromoPriorityAndExpiry(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expired), func(t *testing.T) {
			t.Parallel()
			start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			f := newPassQAFixture(t, passQAPromoTiers(start), true)
			first, second := f.user(t, 26201), f.user(t, 26202)
			f.click(t, first, f.menu(t, first))
			selectedFirst, expiresFirst, err := f.priority(t, first)
			require.NoError(t, err)
			firstRole := f.click(t, first, f.menu(t, first))
			selectedAgain, expiresAgain, err := f.priority(t, first)
			require.NoError(t, err)
			require.Equal(t, selectedFirst, selectedAgain)
			require.Equal(t, expiresFirst, expiresAgain)
			firstForm := f.solo(t, first, firstRole)
			secondForm := f.solo(t, second, f.click(t, second, f.menu(t, second)))
			selectedSecond, _, err := f.priority(t, second)
			require.NoError(t, err)
			require.True(t, selectedSecond.After(selectedFirst))
			f.completeDetails(t, second, secondForm)
			later := f.registration(t, second)
			require.Equal(t, registrations.Waitlisted, later.State)
			require.Nil(t, later.Pass)
			if expired {
				// Shorten the hold after admission so the regular tier cannot affect the result.
				result, expireErr := f.h.store.Pool().Exec(t.Context(), "UPDATE shuttle_pass_promo_choices "+
					"SET expires_at=selected_at+interval '1 microsecond' WHERE event_id=$1 AND user_id=$2",
					f.event.ID, first.app.UserID)
				require.NoError(t, expireErr)
				require.EqualValues(t, 1, result.RowsAffected())
				f.pass(t, second, "promo", 7000)
				f.completeDetails(t, first, firstForm)
				finished := f.registration(t, first)
				require.Nil(t, finished.Pass)
				doc := f.event.Document
				doc.Registration.Pricing.Tiers[1].StartsAt = time.Now().UTC().Truncate(time.Microsecond)
				f.saveEvent(t, doc)
				regular := f.pass(t, first, "regular", 15000)
				require.Equal(t, finished.RequestedAt, regular.RequestedAt)
			} else {
				f.completeDetails(t, first, firstForm)
				earlier := f.pass(t, first, "promo", 7000)
				require.True(t, earlier.RequestedAt.After(later.RequestedAt),
					"the promo winner completed registration second")
				require.Nil(t, f.registration(t, second).Pass)
			}
			f.noLoom(t)
		})
	}
}

func TestPassSaleQAConcurrentLastPromoSlot(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), false)
	first, second := f.namedUser(t, 26301), f.namedUser(t, 26302)
	firstEvent, secondEvent := f.menu(t, first), f.menu(t, second)
	click1, after1 := f.accept(t, first, "", firstEvent.messageID, firstEvent.button.CallbackData)
	click2, after2 := f.accept(t, second, "", secondEvent.messageID, secondEvent.button.CallbackData)
	f.h.await(t, click1, "completed")
	f.h.await(t, click2, "completed")
	leader := passcopy.Text("en", "entry.leader")
	solo := passcopy.Text("en", "entry.solo")
	firstChoice := f.button(t, first, f.click(t, first, f.button(t, first, after1, leader)), solo)
	secondChoice := f.button(t, second, f.click(t, second, f.button(t, second, after2, leader)), solo)
	var firstPriority uuid.UUID
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT user_id FROM shuttle_pass_promo_choices "+
		"WHERE event_id=$1 ORDER BY selected_at, sequence LIMIT 1", f.event.ID).Scan(&firstPriority))
	winner, runnerUp := first, second
	if firstPriority == second.app.UserID {
		winner, runnerUp = second, first
	}
	join1, _ := f.accept(t, first, "", firstChoice.messageID, firstChoice.button.CallbackData)
	join2, _ := f.accept(t, second, "", secondChoice.messageID, secondChoice.button.CallbackData)
	f.h.await(t, join1, "completed")
	f.h.await(t, join2, "completed")
	f.pass(t, winner, "promo", 7000)
	require.Nil(t, f.registration(t, runnerUp).Pass)
	var promoAssignments int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_pass_promo_choices "+
		"WHERE event_id=$1 AND awarded_role IS NOT NULL", f.event.ID).Scan(&promoAssignments))
	require.Equal(t, 1, promoAssignments)
	f.noLoom(t)
}

func TestPassSaleQAWebhookClickOrderSurvivesWorkerDelay(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), false)
	first, second := f.namedUser(t, 26601), f.namedUser(t, 26602)
	firstEvent, secondEvent := f.menu(t, first), f.menu(t, second)
	// Delay the first callback's River job after intake commits its normal database state.
	_, err := f.h.store.Pool().Exec(t.Context(), `
CREATE FUNCTION delay_pass_qa_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.kind = 'shuttle_task' THEN
    NEW.state := 'scheduled';
    NEW.scheduled_at := clock_timestamp() + interval '10 minutes';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER delay_pass_qa_job BEFORE INSERT ON river_job
FOR EACH ROW EXECUTE FUNCTION delay_pass_qa_job();
`)
	require.NoError(t, err)
	click, firstRole := f.accept(t, first, "", firstEvent.messageID, firstEvent.button.CallbackData)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "DROP TRIGGER delay_pass_qa_job ON river_job; DROP FUNCTION delay_pass_qa_job()")
	require.NoError(t, err)
	selectedFirst, expires, err := f.priority(t, first)
	require.NoError(t, err, "the verified first click holds priority before its worker starts")
	require.Equal(t, time.Hour, expires.Sub(selectedFirst))

	secondRole := f.click(t, second, secondEvent)
	selectedSecond, _, err := f.priority(t, second)
	require.NoError(t, err)
	require.True(t, selectedFirst.Before(selectedSecond))
	f.solo(t, second, secondRole)
	_, err = f.h.store.AllocatePassQueue(t.Context(), f.scope, f.event.ID)
	require.NoError(t, err)
	require.Nil(t, f.registration(t, second).Pass, "an earlier accepted click blocks the last promo slot")
	held, err := f.h.store.GetTask(t.Context(), click)
	require.NoError(t, err)
	require.Equal(t, "accepted", held.State)
	tag, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE river_job SET scheduled_at=clock_timestamp(), state='available' "+
			"WHERE kind='shuttle_task' AND args->>'task_id'=$1", click.String())
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	f.h.await(t, click, "completed")
	f.solo(t, first, firstRole)
	f.pass(t, first, "promo", 7000)
	require.Nil(t, f.registration(t, second).Pass)
	f.noLoom(t)
}

func TestPassSalePromoPositionsUseAwardedClickOrder(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	tiers := []registrations.Tier{{ID: "promo", Amount: 2, PriceMinor: 7000, Promo: true, StartsAt: start}}
	f := newPassQAFixture(t, tiers, false)
	d := f.event.Document
	d.Registration.LeaderCapacity = 0
	f.saveEvent(t, d)
	first, second := f.namedUser(t, 29501), f.namedUser(t, 29502)
	firstRole := f.click(t, first, f.menu(t, first))
	secondRole := f.click(t, second, f.menu(t, second))
	f.solo(t, second, secondRole)
	f.solo(t, first, firstRole)
	reg1, reg2 := f.registration(t, first), f.registration(t, second)
	require.True(t, reg2.RequestedAt.Before(reg1.RequestedAt))
	owner := paymentScope(t, f.h, first.app.UserID, false)
	position, err := f.h.store.PassQueuePosition(t.Context(), owner, f.event.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, position.Individual)
	require.EqualValues(t, 1, position.Role)
	f.p.mu.Lock()
	f.p.adminRole = true
	f.p.mu.Unlock()
	admin := detailsAppSession(t, f.h, 29503, 244)
	rows := passAdminRowsByID(t, f.h, admin, f.event.ID)
	require.Equal(t, position.Role, rows[reg1.ID].RolePosition)
	require.EqualValues(t, 2, rows[reg2.ID].RolePosition)
}

func TestPassSaleQAOrdinaryOrderUsesCompletedRegistration(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := newPassQAFixture(t, []registrations.Tier{
		{ID: "opening", PriceMinor: 11000, Amount: 1, StartsAt: now.Add(-time.Minute)},
		{ID: "regular", PriceMinor: 15000, Amount: 1, StartsAt: now.Add(time.Hour), BlockedByDate: true},
	}, false)
	initial, late, early := f.namedUser(t, 26401), f.namedUser(t, 26402), f.namedUser(t, 26403)
	f.solo(t, initial, f.click(t, initial, f.menu(t, initial)))
	f.pass(t, initial, "opening", 11000)
	firstBrowse := f.click(t, late, f.menu(t, late))
	_, _, err := f.priority(t, late)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	f.solo(t, early, f.click(t, early, f.menu(t, early)))
	f.solo(t, late, firstBrowse)
	earlyEntry, lateEntry := f.registration(t, early), f.registration(t, late)
	require.True(t, earlyEntry.RequestedAt.Before(lateEntry.RequestedAt))
	require.Nil(t, earlyEntry.Pass)
	require.Nil(t, lateEntry.Pass)
	doc := f.event.Document
	doc.Registration.Pricing.Tiers[1].StartsAt = now
	f.saveEvent(t, doc)
	assigned := f.pass(t, early, "regular", 15000)
	require.Equal(t, earlyEntry.RequestedAt, assigned.RequestedAt)
	require.Nil(t, f.registration(t, late).Pass)
	f.noLoom(t)
}

func TestPassSaleQAExplicitPairSkipsPromoPriority(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), false)
	paired := f.namedUser(t, 26501)
	task, after := f.accept(t, paired, "/passes synthetic --role leader --pair", 0, "")
	f.h.await(t, task, "completed")
	f.button(t, paired, after, passcopy.Text("en", "entry.revoke_invite"))
	_, _, err := f.priority(t, paired)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	entry := f.registration(t, paired)
	require.Equal(t, registrations.Waitlisted, entry.State)
	require.Nil(t, entry.Pass)
	solo := f.namedUser(t, 26502)
	f.solo(t, solo, f.click(t, solo, f.menu(t, solo)))
	f.pass(t, solo, "promo", 7000)
	require.Nil(t, f.registration(t, paired).Pass)
	f.noLoom(t)
}
