package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func menuOwner(t *testing.T, s *db.Store, telegramID int64) db.ProfileScope {
	t.Helper()
	scope := db.ProfileScope{BotID: 1, Organization: "org", Subject: uuid.NewString(), UserID: uuid.New()}
	_, err := s.Pool().Exec(context.Background(), `INSERT INTO shuttle_users
 (id,bot_id,tg_user_id,zitadel_org,zitadel_subject) VALUES($1,$2,$3,$4,$5)`,
		scope.UserID, scope.BotID, telegramID, scope.Organization, scope.Subject)
	require.NoError(t, err)
	_, err = s.Pool().Exec(context.Background(), `INSERT INTO shuttle_user_profiles
 (user_id,version,full_name,first_name,last_name,name_source)
 VALUES($1,1,'Test Owner','Test','Owner','user')`, scope.UserID)
	require.NoError(t, err)
	return scope
}

func TestTelegramMenuFollowsRegistrationAndPassAvailability(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	scope := menuOwner(t, s, 700)
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := s.Pool().Exec(ctx, query, args...)
		require.NoError(t, err)
	}
	availability := func() db.TelegramMenuAvailability {
		t.Helper()
		value, err := s.MenuAvailability(ctx, scope)
		require.NoError(t, err)
		return value
	}
	require.Equal(t, db.TelegramMenuAvailability{}, availability())
	eventID, registrationID := uuid.New(), uuid.New()
	exec(`INSERT INTO shuttle_events(id,bot_id,organization_id,name,timezone,starts_at,ends_at,modules)
 VALUES($1,1,'org','Dance','UTC',now(),now()+interval '2 days',ARRAY['orders','massage'])`, eventID)
	exec(`INSERT INTO shuttle_event_registration_rules(event_id,document)
 VALUES($1,jsonb_build_object('closes_at',now()+interval '1 day'))`, eventID)
	require.False(t, availability().Register, "a configured but disabled registration module is closed")
	exec(`UPDATE shuttle_events SET modules=array_append(modules,'registrations') WHERE id=$1`, eventID)
	require.True(t, availability().Register)
	exec(`UPDATE shuttle_event_registration_rules
 SET document=jsonb_build_object('closes_at',now()-interval '1 second') WHERE event_id=$1`, eventID)
	require.False(t, availability().Register, "closed registration rules hide new registration")
	exec(`UPDATE shuttle_event_registration_rules
 SET document=jsonb_build_object('closes_at',now()+interval '1 day') WHERE event_id=$1`, eventID)
	require.True(t, availability().Register)
	exec(`INSERT INTO shuttle_registrations(id,event_id,user_id,role,state,requested_at)
 VALUES($1,$2,$3,'leader','waitlisted',now())`, registrationID, eventID, scope.UserID)
	require.Equal(t, db.TelegramMenuAvailability{Registrations: true, Pairs: true}, availability())
	exec(`UPDATE shuttle_registrations SET state='assigned' WHERE id=$1`, registrationID)
	require.Equal(t, db.TelegramMenuAvailability{Registrations: true}, availability(),
		"assigned registration alone grants no pass routes")
	exec(`INSERT INTO shuttle_passes(id,registration_id,price_minor,currency,currency_scale,assigned_at,payment_due_at)
 VALUES($1,$2,100,'EUR',2,now(),now()+interval '1 hour')`, uuid.New(), registrationID)
	require.Equal(
		t,
		db.TelegramMenuAvailability{Registrations: true, Orders: true, Massage: true, Payments: true},
		availability(),
	)
	exec(`UPDATE shuttle_events SET modules='{}' WHERE id=$1`, eventID)
	require.Equal(t, db.TelegramMenuAvailability{Registrations: true}, availability())
	exec(`UPDATE shuttle_users SET revoked=true WHERE id=$1`, scope.UserID)
	_, err := s.MenuAvailability(ctx, scope)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	resolved, err := s.MenuRecipientByTelegram(ctx, 1, "org", 700)
	require.NoError(t, err)
	require.True(t, resolved.Revoked, "the refresh worker can clear a revoked owner's previous menu")
}

func TestTelegramMenuRecipientsAreBoundedAndScoped(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	first, second := menuOwner(t, s, 800), menuOwner(t, s, 801)
	unnamed := menuOwner(t, s, 802)
	_, err := s.Pool().Exec(ctx, `DELETE FROM shuttle_user_profiles WHERE user_id=$1`, unnamed.UserID)
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_users SET revoked=true WHERE id=$1`, second.UserID)
	require.NoError(t, err)
	page, err := s.MenuRecipients(ctx, 1, "org", uuid.Nil, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	more, err := s.MenuRecipients(ctx, 1, "org", page[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, more, 1)
	last, err := s.MenuRecipients(ctx, 1, "org", more[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.ElementsMatch(t, []uuid.UUID{first.UserID, second.UserID, unnamed.UserID},
		[]uuid.UUID{page[0].ID, more[0].ID, last[0].ID}, "unnamed and revoked owners remain reachable for menu clearing")
	page, err = s.MenuRecipients(ctx, 1, "org", last[0].ID, 1)
	require.NoError(t, err)
	require.Empty(t, page)
	page, err = s.MenuRecipients(ctx, 1, "other-org", uuid.Nil, 100)
	require.NoError(t, err)
	require.Empty(t, page)
	_, err = s.MenuRecipients(ctx, 1, "org", uuid.Nil, 101)
	require.Error(t, err)
	_, err = s.MenuAvailability(ctx, unnamed)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	wrongSubject := first
	wrongSubject.Subject = "other-subject"
	_, err = s.MenuAvailability(ctx, wrongSubject)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = s.MenuRecipientByTelegram(ctx, 1, "other-org", 800)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
