package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestMassageScheduleRejectsExpiredStaleAndFailedAuthorizationWithoutPublishing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPayment(t, newPeers(t))
	admin := ownerProfileScope(t, f.H, f.AdminID)
	a := db.MassageScheduleChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		Document: massageDocument(f.Event.Document.StartsAt)}
	op := uuid.New()
	p, err := f.H.store.PrepareMassageSchedule(ctx, admin, op, a)
	require.NoError(t, err)
	denied := errors.New("authorization revoked")
	_, err = f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, a, func(context.Context) error { return denied })
	require.ErrorIs(t, err, denied)
	_, err = f.H.store.MassageScheduleOperation(ctx, admin, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.MassageScheduleConfiguration(ctx, admin, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	check := func(context.Context) error { return nil }
	_, err = f.H.store.ChangeMassageSchedule(ctx, admin, op, uuid.New(), a, check)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_massage_schedule_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", p.ID)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, a, check)
	require.ErrorIs(t, err, db.ErrConflict)
	op = uuid.New()
	p, err = f.H.store.PrepareMassageSchedule(ctx, admin, op, a)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_events SET version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, a, check)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.MassageScheduleConfiguration(ctx, admin, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func massageDocument(start time.Time) massage.ScheduleDocument {
	end := start.Add(4 * time.Hour)
	return massage.ScheduleDocument{Currency: "BYN", CurrencyScale: 2, BlockSeconds: 1200, BufferSeconds: 300,
		Services: []massage.ScheduleService{{ID: "short", Name: massage.ScheduleLabel{EN: "Short", RU: "Короткий"},
			Blocks: 1, PriceMinor: 4500}},
		Windows: []massage.ScheduleWindow{{ID: "night", Name: massage.ScheduleLabel{EN: "Night", RU: "Ночь"},
			StartsAt: start, EndsAt: end, Tables: 2}},
		Specialists: []massage.ScheduleSpecialist{{ID: "alex", Name: massage.ScheduleLabel{EN: "Alex", RU: "Алекс"},
			RequiresTable: true, ServiceIDs: []string{"short"},
			WorkSpans: []massage.ScheduleWorkSpan{{WindowID: "night", StartsAt: start, EndsAt: end}}}},
	}
}

func TestMassageSchedulePersistsExactApprovalAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPayment(t, newPeers(t))
	admin := ownerProfileScope(t, f.H, f.AdminID)
	a := db.MassageScheduleChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		Document: massageDocument(f.Event.Document.StartsAt)}
	op := uuid.New()
	p, err := f.H.store.PrepareMassageSchedule(ctx, admin, op, a)
	require.NoError(t, err)
	_, err = f.H.store.MassageScheduleConfiguration(ctx, admin, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "A proposal must not publish configuration")
	retry, err := f.H.store.PrepareMassageSchedule(ctx, admin, op, a)
	require.NoError(t, err)
	require.Equal(t, p, retry)
	changed := a
	changed.Document = massageDocument(f.Event.Document.StartsAt)
	changed.Document.Services[0].PriceMinor++
	_, err = f.H.store.PrepareMassageSchedule(ctx, admin, op, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	identityChecks := 0
	check := func(context.Context) error { identityChecks++; return nil }
	first, err := f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, a, check)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Version)
	require.Equal(t, 1, identityChecks)
	changed.ExpectedVersion = first.Version
	op2 := uuid.New()
	p2, err := f.H.store.PrepareMassageSchedule(ctx, admin, op2, changed)
	require.NoError(t, err)
	second, err := f.H.store.ChangeMassageSchedule(ctx, admin, op2, p2.ID, changed, check)
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Version)
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_massage_schedule_preparations SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1", p.ID)
	require.NoError(t, err)
	replay, err := f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, a, check)
	require.NoError(t, err)
	require.Equal(t, first, replay, "Expired approval replay returns the original result after subsequent edits")
	require.Equal(t, 2, identityChecks, "Successful replay must not perform another write authorization")
	receipt, err := f.H.store.MassageScheduleOperation(ctx, admin, op)
	require.NoError(t, err)
	require.Equal(t, first, receipt)
	_, err = f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, changed, check)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestMassageOwnerCandidatesRequireAssignedPassAndCurrentEventBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPayment(t, newPeers(t))
	admin, owner := ownerProfileScope(t, f.H, f.AdminID), ownerProfileScope(t, f.H, f.OwnerID)
	a := db.MassageScheduleChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		Document: massageDocument(f.Event.Document.StartsAt)}
	op := uuid.New()
	p, err := f.H.store.PrepareMassageSchedule(ctx, admin, op, a)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(ctx, admin, op, p.ID, a, func(context.Context) error { return nil })
	require.NoError(t, err)
	_, err = f.H.store.OwnMassageSchedule(ctx, owner, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Disabled module denies even an assigned unpaid owner")
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_events SET modules=ARRAY['registrations','massage'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	view, err := f.H.store.OwnMassageSchedule(ctx, owner, f.Event.ID)
	require.NoError(t, err)
	require.True(t, view.ScheduleOnly)
	require.False(t, view.ReservationSupported)
	require.Equal(t, "Europe/Minsk", view.Timezone)
	require.Equal(t, "+03:00", view.Document.Windows[0].StartsAt.Format("-07:00"))
	args := db.MassageCandidateArguments{
		EventID:              f.Event.ID,
		ExpectedEventVersion: view.EventVersion,
		ExpectedVersion:      view.Version,
		Query: massage.ScheduleCandidateQuery{WindowID: "night", SpecialistID: "alex", ServiceID: "short",
			From: a.Document.Windows[0].StartsAt, Until: a.Document.Windows[0].EndsAt, Limit: 2},
	}
	page, err := f.H.store.OwnMassageCandidates(ctx, owner, args)
	require.NoError(t, err)
	require.Len(t, page.Candidates, 2)
	require.True(t, page.HasMore)
	require.False(t, page.ReservationSupported)
	args.ExpectedVersion++
	_, err = f.H.store.OwnMassageCandidates(ctx, owner, args)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.OwnMassageSchedule(ctx, admin, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Administrative identity is not a pass entitlement")
	foreign := owner
	foreign.Subject = "foreign-binding"
	_, err = f.H.store.OwnMassageSchedule(ctx, foreign, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_events SET starts_at=starts_at+interval '1 hour',version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.OwnMassageSchedule(ctx, owner, f.Event.ID)
	require.ErrorIs(t, err, db.ErrConflict, "An incompatible event edit must not silently clip the schedule")
	a.ExpectedEventVersion = view.EventVersion + 1
	a.ExpectedVersion = view.Version
	_, err = f.H.store.PrepareMassageSchedule(ctx, admin, uuid.New(), a)
	require.ErrorIs(t, err, db.ErrConflict, "Current locked event bounds also protect administrative edits")
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.OwnMassageSchedule(ctx, owner, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
