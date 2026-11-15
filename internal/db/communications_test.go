package db_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
	"github.com/complynx/zns-chatbot/internal/db"
)

func communicationQueuedAudience(t *testing.T, recipientCount int) (*db.Store, db.PaymentScope, []uuid.UUID) {
	t.Helper()
	s := testStore(t)
	ctx := t.Context()
	var recipients []uuid.UUID
	err := s.Pool().QueryRow(ctx, `WITH inserted AS (
 INSERT INTO shuttle_users(id,bot_id,tg_user_id,zitadel_subject,zitadel_org)
 SELECT gen_random_uuid(),123,n,'subject-'||n,'org' FROM generate_series(1,$1) n RETURNING id
) SELECT array_agg(id ORDER BY id) FROM inserted`, recipientCount+1).Scan(&recipients)
	require.NoError(t, err)
	author := recipients[0]
	scope := db.PaymentScope{BotID: 123, Organization: "org", UserID: author, Subject: "", GlobalAdmin: true}
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, `SELECT zitadel_subject FROM shuttle_users WHERE id=$1`, author).Scan(&scope.Subject),
	)
	a := db.CommunicationArguments{
		Recipients:       recipients[1:],
		Category:         "information",
		AssessedCategory: broadcasts.Information,
		Assessment:       "Event update",
		Text:             "Meet at 18:00",
		Personalized:     true,
	}
	c, err := s.PrepareCommunication(ctx, scope, uuid.New(), a, false)
	require.NoError(t, err)
	_, err = s.DecideCommunication(ctx, scope, c.ID, "keep", false)
	require.NoError(t, err)
	var ids []uuid.UUID
	require.NoError(
		t,
		s.Pool().
			QueryRow(ctx, `SELECT array_agg(id ORDER BY id) FROM shuttle_communication_deliveries
 WHERE communication_id=$1 AND kind='message'`, c.ID).
			Scan(&ids),
	)
	return s, scope, ids
}

func TestCommunicationLargeAudience(t *testing.T) {
	t.Parallel()
	_, _, ids := communicationQueuedAudience(t, 1000)
	require.Len(t, ids, 1000)
}

func TestCommunicationPersonalizationAdmission(t *testing.T) {
	t.Parallel()
	s, scope, ids := communicationQueuedAudience(t, 2)
	ctx := t.Context()
	for _, id := range []uuid.UUID{ids[0], ids[0]} {
		admitted, e := s.AdmitCommunicationPersonalization(ctx, scope.EventScope, id, 1)
		require.NoError(t, e)
		require.True(t, admitted)
	}
	admitted, err := s.AdmitCommunicationPersonalization(ctx, scope.EventScope, ids[1], 1)
	require.NoError(t, err)
	require.False(t, admitted)
	waiting, _, err := s.CommunicationDelivery(ctx, scope.EventScope, ids[1])
	require.NoError(t, err)
	require.NoError(t, s.ScheduleCommunicationPersonalization(ctx, waiting, time.Minute))
	input := json.RawMessage(`{"private":"recipient memory"}`)
	_, _, err = s.SaveCommunicationInput(ctx, scope.EventScope, ids[0], input)
	require.NoError(t, err)
	require.NoError(t, s.SaveCommunicationRun(ctx, ids[0], "saved-run"))
	raw, run, err := s.CommunicationInput(ctx, scope.EventScope, ids[0])
	require.NoError(t, err)
	require.Empty(t, raw)
	require.Equal(t, "saved-run", run)
	d, _, err := s.CommunicationDelivery(ctx, scope.EventScope, ids[0])
	require.NoError(t, err)
	require.NoError(t, s.ScheduleCommunicationPersonalization(ctx, d, time.Minute))
	require.ErrorIs(t, s.ScheduleCommunicationPersonalization(ctx, d, time.Minute), db.ErrConflict)
	require.NoError(t, s.FailCommunication(ctx, d))
	admitted, err = s.AdmitCommunicationPersonalization(ctx, scope.EventScope, ids[1], 1)
	require.NoError(t, err)
	require.True(t, admitted)
	waiting, _, err = s.CommunicationDelivery(ctx, scope.EventScope, ids[1])
	require.NoError(t, err)
	require.Zero(t, waiting.PersonalizationPolls, "admission restarts inference polling after queue waits")
	chat, create, err := s.ClaimCommunicationChat(ctx, ids[1])
	require.NoError(t, err)
	require.Empty(t, chat)
	require.True(t, create)
	chat, create, err = s.ClaimCommunicationChat(ctx, ids[1])
	require.NoError(t, err)
	require.Empty(t, chat)
	require.False(t, create, "a pending chat must not be created twice")
	require.NoError(t, s.SaveCommunicationChat(ctx, ids[1], "isolated-chat"))
	chat, create, err = s.ClaimCommunicationChat(ctx, ids[1])
	require.NoError(t, err)
	require.Equal(t, "isolated-chat", chat)
	require.False(t, create)
}

func TestCommunicationDraftCancelAndExpiry(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	var author uuid.UUID
	require.NoError(
		t,
		s.Pool().
			QueryRow(ctx, `INSERT INTO shuttle_users(id,bot_id,tg_user_id,zitadel_subject,zitadel_org)
 VALUES(gen_random_uuid(),123,1,'author','org') RETURNING id`).
			Scan(&author),
	)
	scope := db.PaymentScope{BotID: 123, Organization: "org", UserID: author, Subject: "author", GlobalAdmin: true}
	a := db.CommunicationArguments{Recipients: []uuid.UUID{author}, Text: "Hello", Category: "personal"}
	c, err := s.PrepareCommunication(ctx, scope, uuid.New(), a, false)
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_communications SET expires_at=now()-interval '1 second' WHERE id=$1`, c.ID)
	require.NoError(t, err)
	_, err = s.DecideCommunication(ctx, scope, c.ID, "keep", false)
	require.ErrorIs(t, err, db.ErrConflict)
	c, err = s.PrepareCommunication(ctx, scope, uuid.New(), a, false)
	require.NoError(t, err)
	c, err = s.DecideCommunication(ctx, scope, c.ID, "cancel", false)
	require.NoError(t, err)
	require.Equal(t, "cancelled", c.State)
	c, err = s.DecideCommunication(ctx, scope, c.ID, "keep", false)
	require.NoError(t, err)
	require.Equal(t, "cancelled", c.State)
}

func TestCommunicationsRightsConsentSubscriptionsAndRecovery(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	author, client, outsider := uuid.New(), uuid.New(), uuid.New()
	for n, id := range []uuid.UUID{author, client, outsider} {
		_, err := s.Pool().
			Exec(ctx, `
INSERT INTO shuttle_users(id,bot_id,tg_user_id,zitadel_subject,zitadel_org)
VALUES($1,123,$2,$3,'org')
`, id, 700+n, id.String())
		require.NoError(t, err)
	}
	event := uuid.New()
	_, err := s.Pool().
		Exec(ctx, `
INSERT INTO
shuttle_events(id,bot_id,organization_id,name,timezone,starts_at,ends_at,modules)
 VALUES($1,123,'org','Event','UTC',now(),now()+interval '3
days',ARRAY['registrations'])
`, event)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(ctx, `
INSERT INTO shuttle_event_authorities(event_id,user_id,role,version)
VALUES($1,$2,'ambassador',1)
`, event, author)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(ctx, `
INSERT INTO shuttle_registrations(id,event_id,user_id,role,state,requested_at)
VALUES($1,$2,$3,'leader','waitlisted',now())
`, uuid.New(), event, client)
	require.NoError(t, err)
	scope := db.PaymentScope{
		BotID: 123, Organization: "org",
		UserID:  author,
		Subject: author.String(),
	}
	a := db.CommunicationArguments{
		EventID:          event,
		Recipients:       []uuid.UUID{client},
		Text:             "Exact **body**\ntext",
		Category:         "warning",
		AssessedCategory: broadcasts.Information,
		Assessment:       "Event logistics, no safety risk",
	}
	op := uuid.New()
	draft, err := s.PrepareCommunication(ctx, scope, op, a, false)
	require.NoError(t, err)
	require.Equal(t, "prepared", draft.State)
	same, err := s.PrepareCommunication(ctx, scope, op, a, false)
	require.NoError(t, err)
	require.Equal(t, draft.ID, same.ID)
	changed := a
	changed.Text = "Changed"
	_, err = s.PrepareCommunication(ctx, scope, op, changed, false)
	require.ErrorIs(t, err, db.ErrConflict)
	bad := a
	bad.Assessment = " \t\n"
	_, err = s.PrepareCommunication(ctx, scope, uuid.New(), bad, false)
	require.ErrorIs(t, err, broadcasts.ErrInvalid)
	bad = a
	bad.Recipients = []uuid.UUID{outsider}
	_, err = s.PrepareCommunication(ctx, scope, uuid.New(), bad, false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	clientScope := scope
	clientScope.UserID = client
	clientScope.Subject = client.String()
	_, err = s.DecideCommunication(ctx, clientScope, draft.ID, "keep", false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	queued, err := s.DecideCommunication(ctx, scope, draft.ID, "suggested", false)
	require.NoError(t, err)
	require.Equal(t, "information", queued.Category)
	_, err = s.DecideCommunication(ctx, scope, draft.ID, "keep", false)
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		s.Pool().
			QueryRow(ctx, `
SELECT count(*) FROM shuttle_communication_deliveries WHERE communication_id=$1 AND
kind='message'
`, draft.ID).
			Scan(&count),
	)
	require.Equal(t, 1, count)
	var deliveryID uuid.UUID
	require.NoError(
		t,
		s.Pool().
			QueryRow(ctx, `
SELECT id FROM shuttle_communication_deliveries WHERE communication_id=$1 AND
kind='message'
`, draft.ID).
			Scan(&deliveryID),
	)
	d, c, err := s.CommunicationDelivery(ctx, scope.EventScope, deliveryID)
	require.NoError(t, err)
	profile := db.ProfileScope{BotID: 123, Organization: "org", UserID: client, Subject: client.String()}
	require.NoError(t, s.SetBroadcastSubscription(ctx, profile, event, true, broadcasts.Information, false))
	_, err = s.ReserveCommunication(ctx, scope, d, c, false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	// An event subscription does not override the global opt-out.
	require.NoError(t, s.SetBroadcastSubscription(ctx, profile, event, false, broadcasts.Information, true))
	_, err = s.ReserveCommunication(ctx, scope, d, c, false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	require.NoError(t, s.SetBroadcastSubscription(ctx, profile, event, true, broadcasts.Information, true))
	reserved, err := s.ReserveCommunication(ctx, scope, d, c, false)
	require.NoError(t, err)
	require.NoError(t, s.RecoverCommunications(ctx, scope.EventScope))
	d, _, err = s.CommunicationDelivery(ctx, scope.EventScope, deliveryID)
	require.NoError(t, err)
	require.Equal(t, "delivery_unknown", d.State)
	require.ErrorIs(
		t,
		s.SetBroadcastSubscription(ctx, profile, event, true, broadcasts.Warning, false),
		broadcasts.ErrInvalid,
	)
	// A stale sender cannot overwrite the recovered uncertainty.
	require.ErrorIs(t, s.FinishCommunication(ctx, reserved, "delivered", nil, 0), db.ErrConflict)
	// Participants may contact their event authorities, but cannot broadcast.
	personal := db.CommunicationArguments{
		EventID:    event,
		Recipients: []uuid.UUID{author},
		Category:   "personal",
		Text:       "Question",
	}
	_, err = s.PrepareCommunication(ctx, clientScope, uuid.New(), personal, false)
	require.NoError(t, err)
	_, err = s.PrepareCommunication(ctx, clientScope, uuid.New(), a, false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	page, err := s.CommunicationAudience(
		ctx,
		scope,
		db.CommunicationAudienceQuery{EventID: event, RegistrationState: "waitlisted"},
	)
	require.NoError(t, err)
	require.Len(t, page.Users, 1)
	contacts, err := s.CommunicationAudience(
		ctx,
		clientScope,
		db.CommunicationAudienceQuery{EventID: event, Contacts: true},
	)
	require.NoError(t, err)
	require.Len(t, contacts.Users, 1)
	require.Equal(t, author, contacts.Users[0].UserID)
}

func TestCommunicationMigrationPreservesPopulatedUpgrade(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	provider := baselineProvider(t, s)
	_, err := provider.DownTo(ctx, 81)
	require.NoError(t, err)
	task := accepted(t, s, 19)
	before, err := s.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Migrate(ctx))
	after, err := s.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = s.Pool().
		Exec(ctx, `
INSERT INTO shuttle_broadcast_subscriptions(user_id,scope,category,subscribed)
VALUES($1,'all','information',false)
`, task.UserID)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 81)
	require.ErrorContains(t, err, "history and subscriptions require")
	var subscribed bool
	require.NoError(
		t,
		s.Pool().
			QueryRow(ctx, `
SELECT subscribed FROM shuttle_broadcast_subscriptions WHERE user_id=$1
`, task.UserID).
			Scan(&subscribed),
	)
	require.False(t, subscribed)
}
