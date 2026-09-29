package integration_test

import (
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func massageBotFixture(t *testing.T) *fixture {
	t.Helper()
	db, _, _ := massageFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	server := httptest.NewServer(
		api.Handler(runtimeapp.NewServices(db, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	t.Cleanup(server.Close)
	fake, err := sandbox.New(t.Context(), db, "sandbox")
	require.NoError(t, err)
	telegramServer := httptest.NewServer(fake.Handler())
	t.Cleanup(telegramServer.Close)
	return &fixture{db: db, fake: telegramServer, b: &bot.Bot{
		DB: db, API: bot.APIClient{Base: server.URL, Signer: signer},
		TG: telegram.Client{Base: telegramServer.URL, Token: "sandbox"},
	}}
}

func TestMassageBotSpecialistRussianPreferencesInstantAndDelivery(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	start := time.Now().UTC().Truncate(20 * time.Minute)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.massage_parties SET starts_at=$1,ends_at=$2`,
		start,
		start.Add(4*time.Hour),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.massage_work SET starts_at=$1,ends_at=$2`,
		start,
		start.Add(4*time.Hour),
	)
	require.NoError(t, err)
	handle(t, f.b, message(200, 202, "/massage"))
	assert.Contains(t, massageCard(t, f, 202).Text, "Массаж")
	handle(t, f.b, massageClick(t, f, 202, 201, "Уведомления"))
	service := massage.Service{DB: f.db}
	before, err := service.Preferences(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	card := massageCard(t, f, 202)
	button := card.Markup.Rows[0][0]
	handle(t, f.b, massageClick(t, f, 202, 202, button.Text))
	after, err := service.Preferences(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	assert.NotEqual(t, before.Bookings, after.Bookings)
	_, err = service.SetPreferences(
		t.Context(),
		"bob",
		"sandbox-festival",
		massage.Preferences{Bookings: true, Next: true},
	)
	require.NoError(t, err)
	booking, err := service.Execute(t.Context(), "alice", massageBook("delivery", "bob", 4, 1))
	require.NoError(t, err)
	base := f.b.TG.Base
	f.b.TG.Base = f.fake.URL + "/unavailable"
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	pending, err := service.PendingNotices(t.Context(), "bob")
	require.NoError(t, err)
	require.NotEmpty(t, pending)
	f.b.TG.Base = base
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	var noticeID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.massage_notices WHERE booking_id=$1 AND kind='booked' AND sent_at IS NOT NULL`, booking.ID).
			Scan(&noticeID),
	)
	initial := len(chatMessages(t, f, 202))
	_, err = f.db.Exec(t.Context(), `UPDATE core.massage_notices SET sent_at=NULL WHERE id=$1`, noticeID)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 202), initial)
	handle(t, f.b, message(210, 202, "/massage"))
	handle(t, f.b, massageClick(t, f, 202, 211, "Мои клиенты"))
	verifyMassageContactAndLateReminder(t, f, booking.ID)
	handle(t, f.b, message(212, 202, "/massage"))
	handle(t, f.b, massageClick(t, f, 202, 213, "Занять время сейчас"))
	card = massageCard(t, f, 202)
	require.GreaterOrEqual(t, len(card.Markup.Rows), 6)
	handle(t, f.b, massageClick(t, f, 202, 214, "15 мин"))
	var instant bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT instant FROM core.massage_bookings WHERE owner='bob'`).Scan(&instant),
	)
	assert.True(t, instant)
}

func massageCard(t *testing.T, f *fixture, user int64) telegram.Message {
	t.Helper()
	var id int64
	err := f.db.QueryRow(t.Context(), `SELECT v.message_id FROM bot.massage_views v
	JOIN core.users u ON u.id=v.owner WHERE u.telegram_id=$1`, user).Scan(&id)
	require.NoError(t, err)
	for _, message := range chatMessages(t, f, user) {
		if message.ID == id {
			return message
		}
	}
	t.Fatal("massage card missing")
	return telegram.Message{}
}

func massageClick(t *testing.T, f *fixture, user, id int64, prefix string) telegram.Update {
	t.Helper()
	card := massageCard(t, f, user)
	for _, row := range card.Markup.Rows {
		for _, button := range row {
			if strings.HasPrefix(button.Text, prefix) {
				return telegram.Update{ID: id, Callback: &telegram.Callback{
					ID: strconv.FormatInt(id, 10), From: telegram.User{ID: user}, Message: card, Data: button.Data,
				}}
			}
		}
	}
	t.Fatalf("missing massage button %q in %+v", prefix, card.Markup)
	return telegram.Update{}
}

func TestMassageBotBookStaleForeignCancelAndRestart(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='en' WHERE id='alice'`)
	require.NoError(t, err)
	handle(t, f.b, message(100, 101, "/massage"))
	initial := massageCard(t, f, 101).ID
	for index, label := range []string{"Book a massage", "02.10", "15 min", "Bob"} {
		handle(t, f.b, massageClick(t, f, 101, int64(101+index), label))
	}
	booking := massageClick(t, f, 101, 105, "02.10")
	foreign := booking
	foreign.ID = 106
	copyCallback := *booking.Callback
	copyCallback.From.ID = 303
	foreign.Callback = &copyCallback
	handle(t, f.b, foreign)
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_bookings`).Scan(&count))
	assert.Zero(t, count)
	handle(t, f.b, booking)
	card := massageCard(t, f, 101)
	assert.Equal(t, initial, card.ID)
	assert.Contains(t, card.Text, "Booking saved")
	assert.Contains(t, card.Text, "43 BYN")
	booking.ID = 107
	booking.Callback.ID = "107"
	handle(t, f.b, booking)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_bookings`).Scan(&count))
	assert.Equal(t, 1, count)
	assert.Contains(t, massageCard(t, f, 101).Text, "outdated")
	handle(t, f.b, massageClick(t, f, 101, 108, "Cancel "))
	assert.Contains(t, massageCard(t, f, 101).Text, "Cancelled")
	restarted := *f.b
	require.NoError(t, restarted.RenderMassage(t.Context(), "alice", 101, ""))
	assert.Equal(t, initial, massageCard(t, f, 101).ID)
	var buttons int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.massage_buttons WHERE owner='alice'`).Scan(&buttons),
	)
	assert.LessOrEqual(t, buttons, 2)
}

func TestMassageBotSlotPagesAndOrderCardIsolation(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='en' WHERE id='alice'`)
	require.NoError(t, err)
	handle(t, f.b, message(300, 101, "/massage"))
	for index, label := range []string{"Book a massage", "02.10", "15 min", "Bob"} {
		handle(t, f.b, massageClick(t, f, 101, int64(301+index), label))
	}
	first := massageCard(t, f, 101)
	handle(t, f.b, massageClick(t, f, 101, 305, "Next"))
	second := massageCard(t, f, 101)
	assert.Equal(t, first.ID, second.ID)
	assert.NotEqual(t, first.Markup, second.Markup)
	handle(t, f.b, massageClick(t, f, 101, 306, "Previous"))
	var buttons int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.massage_buttons WHERE owner='alice'`).Scan(&buttons),
	)
	assert.LessOrEqual(t, buttons, 10)
	handle(t, f.b, message(307, 101, "/orders"))
	assert.Equal(t, first.Text, massageCard(t, f, 101).Text)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.massage_work`)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderMassage(t.Context(), "alice", 101, ""))
	assert.Len(t, massageCard(t, f, 101).Markup.Rows, 1)
}

func verifyMassageContactAndLateReminder(t *testing.T, f *fixture, bookingID string) {
	t.Helper()
	contacts := massageCard(t, f, 202).Markup.Rows
	foundContact := false
	for _, row := range contacts {
		for _, button := range row {
			if button.URL == "tg://user?id=101" {
				foundContact = true
			}
		}
	}
	assert.True(t, foundContact)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_notices(booking_id,owner,kind) VALUES($1,'alice','prior_short');
	`,
		bookingID,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.massage_bookings SET starts_at=now()-interval '20 minutes',ends_at=now()-interval '5 minutes' WHERE id=$1`,
		bookingID,
	)
	require.NoError(t, err)
	aliceMessages := len(chatMessages(t, f, 101))
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 101), aliceMessages+2)
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 101), aliceMessages+2)
}
