package integration_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func passMenuFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at,titles) VALUES('dance',now()+interval '30 days','{"en":"Dance","ru":"Танцы"}');
	INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',0,20,100,now()-interval '1 day');
	INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','bob');
	INSERT INTO core.pass_booking_admins(owner) VALUES('bob');
	INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader'),('bob','follower');
	UPDATE core.users SET language='en' WHERE id='alice'`,
	)
	require.NoError(t, err)
	return f
}

func TestPassMenuProfileControlsBindOwnerAndVersion(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	handlePassVisible(t, f, message(600, 101, "/role"))
	choice := orderClick(t, f, 101, 601, "Follower")
	foreign := choice
	copyCallback := *choice.Callback
	copyCallback.From.ID = 202
	foreign.Callback = &copyCallback
	foreign.ID = 602
	handlePassVisible(t, f, foreign)
	s := passes.Service{DB: f.db}
	before, err := s.Get(t.Context(), "bob")
	require.NoError(t, err)
	assert.Zero(t, before.Version)
	handlePassVisible(t, f, choice)
	profile, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "follower", profile.Role)
	choice.ID = 603
	choice.Callback.ID = "603"
	handlePassVisible(t, f, choice)
	after, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, profile.Version, after.Version)
	handlePassVisible(t, f, message(604, 101, "/passport"))
	profile, err = s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "passport", profile.Pending)
	var shown bool
	for _, card := range chatMessages(t, f, 101) {
		if strings.Contains(card.Text, "Send your passport details") {
			shown = true
		}
	}
	assert.True(t, shown)
}

func TestPassMenuQueueTraversesAPIPagesAndClampsAfterChanges(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.users(id,telegram_id,name) SELECT 'pass-page-'||i,50000+i,'Client '||i FROM generate_series(1,31) i;
	INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
	SELECT 'dance','pass-page-'||i,1,'waitlist','leader','solo','bob',now()+i*interval '1 second' FROM generate_series(1,31) i`,
	)
	require.NoError(t, err)
	handlePassVisible(t, f, message(700, 202, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 701, "Танцы"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 702, "Очередь регистрации"))
	for update := int64(703); update < 709; update++ {
		handlePassVisible(t, f, passMenuClick(t, f, 202, update, "Далее"))
	}
	assert.Contains(t, passMenuCard(t, f, 202).Text, "50031")
	handlePassVisible(t, f, passMenuClick(t, f, 202, 709, "Назад"))
	assert.Contains(t, passMenuCard(t, f, 202).Text, "50026")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_bookings WHERE owner LIKE 'pass-page-%'`)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderPassMenu(t.Context(), "bob", 202, ""))
	drainPassNotices(t, f)
	assert.NotContains(t, passMenuCard(t, f, 202).Text, "50026")
	handlePassVisible(t, f, passMenuClick(t, f, 202, 710, "К началу списка"))
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='bob'`).Scan(&count),
	)
	assert.LessOrEqual(t, count, 3)
}

func passMenuCard(t *testing.T, f *fixture, user int64) telegram.Message {
	t.Helper()
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT v.message_id FROM bot.pass_views v JOIN core.users u ON u.id=v.owner WHERE u.telegram_id=$1`, user).
			Scan(&id),
	)
	for _, message := range chatMessages(t, f, user) {
		if message.ID == id {
			return message
		}
	}
	t.Fatal("pass menu card missing")
	return telegram.Message{}
}

func passMenuClick(t *testing.T, f *fixture, user, id int64, label string) telegram.Update {
	t.Helper()
	card := passMenuCard(t, f, user)
	for _, row := range card.Markup.Rows {
		for _, button := range row {
			if strings.HasPrefix(button.Text, label) && button.Data != "" {
				return telegram.Update{
					ID: id,
					Callback: &telegram.Callback{
						ID:      strconv.FormatInt(id, 10),
						From:    telegram.User{ID: user},
						Message: card,
						Data:    button.Data,
					},
				}
			}
		}
	}
	t.Fatalf("pass button %q missing in %+v", label, card.Markup)
	return telegram.Update{}
}

func TestPassMenuSoloForeignStaleCancelPersistence(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	handlePassVisible(t, f, message(400, 101, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 401, "Dance"))
	register := passMenuClick(t, f, 101, 402, "Register solo")
	foreign := register
	copyCallback := *register.Callback
	copyCallback.From.ID = 303
	foreign.Callback = &copyCallback
	foreign.ID = 403
	handlePassVisible(t, f, foreign)
	s := passbooking.Service{DB: f.db, Delivery: f.b.Delivery}
	booking, err := s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Zero(t, booking.Version)
	handlePassVisible(t, f, register)
	booking, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Positive(t, booking.Version)
	register.ID = 404
	register.Callback.ID = "404"
	handlePassVisible(t, f, register)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "outdated")
	handlePassVisible(t, f, passMenuClick(t, f, 101, 405, "Cancel registration"))
	assert.Contains(t, passMenuCard(t, f, 101).Text, "Cancelled")
	card := passMenuCard(t, f, 101)
	restarted := *f.b
	require.NoError(t, restarted.RenderPassMenu(t.Context(), "alice", 101, ""))
	pumpBotDeliveries(t, &restarted)
	assert.Equal(t, card.ID, passMenuCard(t, f, 101).ID)
}

func TestPassMenuRussianInvitationAndAdminQueue(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	s := passbooking.Service{DB: f.db, Delivery: f.b.Delivery}
	command := bookingCommand("invite", "menu-invite", passbooking.Booking{})
	command.InviteTelegramID = 202
	_, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	handlePassVisible(t, f, message(500, 202, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 501, "Танцы"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 502, "Приглашения"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 503, "Принять"))
	booking, err := s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "alice", booking.Partner)
	handlePassVisible(t, f, passMenuClick(t, f, 202, 504, "Меню регистрации"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 505, "Очередь регистрации"))
	assert.Contains(t, passMenuCard(t, f, 202).Text, "Выделен")
	handlePassVisible(t, f, passMenuClick(t, f, 202, 506, "Разъединить пару"))
	booking, err = s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Empty(t, booking.Partner)
}
