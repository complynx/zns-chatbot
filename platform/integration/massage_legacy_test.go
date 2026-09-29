package integration_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const legacyDraftID = "1234567890abcdef12345678"

func legacyMassageDraft(t *testing.T, db *pgxpool.Pool, state string) {
	t.Helper()
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_massage_import_references(source_key,bot_id,source_kind,source_record_sha256,source_record,target_id,owner,event_id)
 VALUES($1,77,'draft',$2,'{"_id":{"$oid":"1234567890abcdef12345678"}}','draft',$3,'sandbox-festival');`,
		strings.Repeat("a", 64),
		strings.Repeat("b", 64),
		"alice",
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_massage_drafts(id,source_key,owner,event_id,state) VALUES('draft',$1,$2,'sandbox-festival',$3)`,
		strings.Repeat("a", 64),
		"alice",
		state,
	)
	require.NoError(t, err)
}

func TestLegacyMassageDraftContinuationAndConcurrency(t *testing.T) {
	t.Parallel()
	db, s, _ := massageFixture(t)
	legacyMassageDraft(
		t,
		db,
		`{"party":"night","length":1,"page":2,"selected":{"bob":false,"master":true},"choices":{"4":{"specialist":"bob"}}}`,
	)
	c := massage.LegacyCommand{ID: legacyDraftID, Event: "sandbox-festival", Key: "source", Action: "source", Choice: 4}
	_, err := s.ExecuteLegacy(t.Context(), "client", c)
	require.Error(t, err)
	d, err := s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	assert.True(t, d.State.Selected["bob"])
	assert.Equal(t, 2, d.State.Page, "source filter changes retain the saved page")
	assert.EqualValues(t, 1, d.Version)
	_, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	c.Key = "older-source"
	d, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	assert.True(t, d.State.Selected["bob"], "old source callback cannot toggle modern state")
	slot := 2
	book := massage.LegacyCommand{
		ID:        "draft",
		Event:     c.Event,
		Action:    "select",
		Version:   1,
		Selection: massage.LegacyChoice{Specialist: "bob", Slot: &slot},
	}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, key := range []string{"one", "two"} {
		wg.Go(func() {
			copyCommand := book
			copyCommand.Key = key
			_, bookErr := s.ExecuteLegacy(t.Context(), "alice", copyCommand)
			errors <- bookErr
		})
	}
	wg.Wait()
	close(errors)
	for bookErr := range errors {
		require.NoError(t, bookErr)
	}
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_bookings WHERE owner='alice'`).Scan(&count),
	)
	assert.Equal(t, 1, count)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices WHERE kind='booked'`).Scan(&count),
	)
	assert.Equal(t, 1, count)
	d, err = s.LegacyDraft(t.Context(), "alice", c.Event, "draft")
	require.NoError(t, err)
	assert.True(t, d.Closed)
	assert.NotEmpty(t, d.Booking)
}

func TestLegacyMassageDraftCurrentAvailabilityAndVersions(t *testing.T) {
	t.Parallel()
	db, s, _ := massageFixture(t)
	legacyMassageDraft(
		t,
		db,
		`{"party":"night","length":1,"page":0,"selected":{"bob":true},"choices":{"1":{"slot":2,"specialist":"bob"}}}`,
	)
	_, err := s.Execute(t.Context(), "client", massageBook("occupy", "bob", 2, 1))
	require.NoError(t, err)
	c := massage.LegacyCommand{ID: legacyDraftID, Event: "sandbox-festival", Key: "source", Action: "source", Choice: 1}
	_, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.Error(t, err)
	d, err := s.LegacyDraft(t.Context(), "alice", c.Event, "draft")
	require.NoError(t, err)
	assert.Zero(t, d.Version, "failed booking must retain resumable state")
	c.Choice = 0
	c.Key = "resume"
	d, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	c.Action = "back"
	c.Key = "stale"
	c.Version = 0
	_, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.Error(t, err)
	c.Version = d.Version
	c.Key = "back"
	d, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	assert.Zero(t, d.State.Length)
}

func TestLegacyMassageTelegramResumeBothLocales(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			f := massageBotFixture(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", locale, false)
			require.NoError(t, err)
			legacyMassageDraft(
				t,
				f.db,
				`{"party":"night","length":1,"page":0,"selected":{"bob":false,"master":true},"choices":{}}`,
			)
			update := telegram.Update{
				ID: 800,
				Callback: &telegram.Callback{
					ID:      "legacy",
					From:    telegram.User{ID: 101},
					Data:    "massage|ed|" + legacyDraftID + "|0",
					Message: telegram.Message{ID: 1, Chat: telegram.Chat{ID: 101, Type: "private"}},
				},
			}
			handle(t, f.b, update)
			card := massageCard(t, f, 101)
			assert.Contains(t, card.Text, map[string]string{"en": "Massage", "ru": "Массаж"}[locale])
			handle(t, f.b, massageClick(t, f, 101, 801, "❌ Bob"))
			var enabled bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT (state->'selected'->>'bob')::boolean FROM core.legacy_massage_drafts WHERE id='draft'`).
					Scan(&enabled),
			)
			assert.True(t, enabled)
		})
	}
}

func TestLegacyMassageBookingCancellationUsesCurrentExecutor(t *testing.T) {
	t.Parallel()
	db, s, _ := massageFixture(t)
	booking, err := s.Execute(t.Context(), "alice", massageBook("imported", "bob", 2, 1))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_massage_import_references(source_key,bot_id,source_kind,source_record_sha256,source_record,target_id,owner,event_id)
 VALUES($1,77,'booking',$2,'{"_id":{"$oid":"1234567890abcdef12345678"}}',$3,'alice','sandbox-festival')`,
		strings.Repeat("c", 64),
		strings.Repeat("d", 64),
		booking.ID,
	)
	require.NoError(t, err)
	c := massage.LegacyCommand{
		ID:     legacyDraftID,
		Event:  "sandbox-festival",
		Key:    "cancel-old",
		Action: "source",
		Choice: 1,
	}
	_, err = s.ExecuteLegacy(t.Context(), "client", c)
	require.Error(t, err)
	d, err := s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	assert.EqualValues(t, 2, d.Version)
	assert.True(t, d.Cancelled)
	_, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices WHERE booking_id=$1 AND kind='cancelled'`, booking.ID).
			Scan(&count),
	)
	assert.Equal(t, 1, count)
	c.Key = "other-old-button"
	_, err = s.ExecuteLegacy(t.Context(), "alice", c)
	require.Error(t, err, "another source callback cannot silently assume the current booking version")
}

func TestLegacyMassagePractitionerControls(t *testing.T) {
	t.Parallel()
	db, s, start := massageFixture(t)
	_, err := s.LegacyTogglePreferences(t.Context(), "alice", "sandbox-festival", "forbidden", 51)
	require.Error(t, err)
	prefs, err := s.LegacyTogglePreferences(t.Context(), "bob", "sandbox-festival", "toggle", 51)
	require.NoError(t, err)
	assert.False(t, prefs.Bookings)
	prefs, err = s.LegacyTogglePreferences(t.Context(), "bob", "sandbox-festival", "toggle", 51)
	require.NoError(t, err)
	assert.False(t, prefs.Bookings)
	_, err = s.LegacyTogglePreferences(t.Context(), "bob", "sandbox-festival", "toggle", 52)
	require.Error(t, err)
	s.Now = func() time.Time { return start.Add(10 * time.Minute) }
	booking, err := s.LegacyInstant(t.Context(), "bob", "sandbox-festival", "instant", 1)
	require.NoError(t, err)
	assert.True(t, booking.Instant)
	s.Now = func() time.Time { return start.Add(30 * time.Hour) }
	replayed, err := s.LegacyInstant(t.Context(), "bob", "sandbox-festival", "instant", 1)
	require.NoError(t, err)
	assert.Equal(t, booking.ID, replayed.ID, "replay retains original party after the event ends")
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_massage_import_references(source_key,bot_id,source_kind,source_record_sha256,source_record,target_id,owner,event_id)
 VALUES($1,77,'booking',$2,'{"_id":{"$oid":"1234567890abcdef12345678"}}',$3,'bob','sandbox-festival')`,
		strings.Repeat("e", 64),
		strings.Repeat("f", 64),
		booking.ID,
	)
	require.NoError(t, err)
	visible, err := s.LegacyPractitionerBooking(t.Context(), "bob", "sandbox-festival", legacyDraftID)
	require.NoError(t, err)
	assert.Equal(t, booking.ID, visible.ID)
	_, err = s.LegacyPractitionerBooking(t.Context(), "master", "sandbox-festival", legacyDraftID)
	require.Error(t, err, "another practitioner cannot open a private imported booking")
}

func TestLegacyMassagePractitionerTelegramCallbacks(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	callback := func(id int64, data string) telegram.Update {
		return telegram.Update{
			ID: id,
			Callback: &telegram.Callback{
				ID:      strconv.FormatInt(id, 10),
				From:    telegram.User{ID: 202},
				Data:    data,
				Message: telegram.Message{ID: 1, Chat: telegram.Chat{ID: 202, Type: "private"}},
			},
		}
	}
	handle(t, f.b, callback(850, "massage|notifications|51"))
	assert.Contains(t, massageCard(t, f, 202).Text, "Уведомления")
	var enabled bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT notify_bookings FROM core.massage_specialists WHERE owner='bob'`).
			Scan(&enabled),
	)
	assert.False(t, enabled)
	start := time.Now().UTC().Truncate(20 * time.Minute)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.massage_parties SET id='legacy-massage-party:sandbox-festival:2',starts_at=$1,ends_at=$2`,
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
	handle(t, f.b, callback(851, "massage|instant|1"))
	var booking string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.massage_bookings WHERE owner='bob' AND instant`).Scan(&booking),
	)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_massage_import_references(source_key,bot_id,source_kind,source_record_sha256,source_record,target_id,owner,event_id)
 VALUES($1,77,'booking',$2,'{"_id":{"$oid":"1234567890abcdef12345678"}}',$3,'bob','sandbox-festival')`,
		strings.Repeat("e", 64),
		strings.Repeat("f", 64),
		booking,
	)
	require.NoError(t, err)
	handle(t, f.b, callback(852, "massage|clientlist|2"))
	handle(t, f.b, callback(853, "massage|sped|"+legacyDraftID))
	assert.NotEmpty(t, massageCard(t, f, 202).Markup.Rows)
	_, err = f.b.API.ExecuteMassage(
		t.Context(),
		"bob",
		massage.Command{
			Key:     "cancel-after-import",
			Action:  "cancel",
			Event:   "sandbox-festival",
			Booking: booking,
			Version: 1,
		},
	)
	require.NoError(t, err)
	handle(t, f.b, callback(854, "massage|sped|"+legacyDraftID))
	assert.Contains(t, massageCard(t, f, 202).Text, "Отменено")
}
