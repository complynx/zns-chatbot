package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func bindingFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.MassageBindingChange, db.MassageScheduleChange) {
	t.Helper()
	f, _, _, schedule := massageBookingFixture(t, 1)
	actor := ownerProfileScope(t, f.H, f.AdminID)
	current, err := f.H.store.MassageScheduleConfiguration(t.Context(), actor, f.Event.ID)
	require.NoError(t, err)
	return f, actor, db.MassageBindingChange{
		EventID:                 f.Event.ID,
		SpecialistID:            "alex",
		UserID:                  f.OwnerID,
		Active:                  true,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: current.Version,
	}, schedule
}

func bindingChange(
	t *testing.T,
	f paymentFixture,
	actor db.ProfileScope,
	args db.MassageBindingChange,
) db.MassageSpecialistBinding {
	t.Helper()
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, op, args)
	require.NoError(t, err)
	result, err := f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	return result
}

func TestMassageBindingGrantRevokeAndOriginalReceiptSurviveNewerChanges(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, op, args)
	require.NoError(t, err)
	grant, err := f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.True(t, grant.Active)
	require.Equal(t, int64(1), grant.Version)
	args.Active = false
	args.ExpectedVersion = grant.Version
	revoked := bindingChange(t, f, actor, args)
	require.False(t, revoked.Active)
	args.Active = true
	args.ExpectedVersion = revoked.Version
	again := bindingChange(t, f, actor, args)
	require.True(t, again.Active)
	require.Equal(t, int64(3), again.Version)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET "+
			"expires_at=clock_timestamp()-interval '1 second' WHERE actor_id=$1 AND operation_id=$2", actor.UserID, op)
	require.NoError(t, err)
	original := args
	original.ExpectedVersion = 0
	replay, err := f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		original,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.Equal(t, grant, replay)
	receipt, err := f.H.store.MassageBindingOperation(t.Context(), actor, op)
	require.NoError(t, err)
	require.Equal(t, grant, receipt)
	original.Active = false
	_, err = f.H.store.PrepareMassageBinding(t.Context(), actor, op, original)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestMassageBindingDeniedAndExpiredApprovalDoNotGrantAccess(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, op, args)
	require.NoError(t, err)
	denied := errors.New("IAM administrator revoked")
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		args,
		func(context.Context) error { return denied },
	)
	require.ErrorIs(t, err, denied)
	_, err = f.H.store.MassageBindingOperation(t.Context(), actor, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET "+
			"expires_at=clock_timestamp()-interval '1 second' WHERE actor_id=$1 AND operation_id=$2", actor.UserID, op)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrConflict)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_specialist_bindings").Scan(&count),
	)
	require.Zero(t, count)
}

func TestMassageBindingCannotTransferAliasAndRemovalDoesNotResurrectAccess(t *testing.T) {
	t.Parallel()
	f, actor, args, schedule := bindingFixture(t)
	grant := bindingChange(t, f, actor, args)
	other := schedule.Document.Specialists[0]
	other.ID = "other"
	schedule.Document.Specialists = append(schedule.Document.Specialists, other)
	schedule.ExpectedVersion = args.ExpectedScheduleVersion
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageSchedule(t.Context(), actor, op, schedule)
	require.NoError(t, err)
	updated, err := f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		op,
		prep.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	var active bool
	var version int64
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT active,version FROM shuttle_massage_specialist_bindings WHERE event_id=$1 "+
				"AND specialist_id='alex'", args.EventID).
			Scan(&active, &version),
	)
	require.True(t, active)
	require.Equal(t, grant.Version, version)
	original := schedule.Document.Specialists[0]
	schedule.Document.Specialists = schedule.Document.Specialists[1:]
	schedule.ExpectedVersion = updated.Version
	op = uuid.New()
	prep, err = f.H.store.PrepareMassageSchedule(t.Context(), actor, op, schedule)
	require.NoError(t, err)
	updated, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		op,
		prep.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT active,version FROM shuttle_massage_specialist_bindings WHERE event_id=$1 "+
				"AND specialist_id='alex'", args.EventID).
			Scan(&active, &version),
	)
	require.False(t, active)
	require.Equal(t, int64(2), version)
	schedule.Document.Specialists = append(schedule.Document.Specialists, original)
	schedule.ExpectedVersion = updated.Version
	op = uuid.New()
	prep, err = f.H.store.PrepareMassageSchedule(t.Context(), actor, op, schedule)
	require.NoError(t, err)
	updated, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		op,
		prep.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT active FROM shuttle_massage_specialist_bindings WHERE event_id=$1 AND "+
				"specialist_id='alex'", args.EventID).
			Scan(&active),
	)
	require.False(t, active)
	args.ExpectedVersion = 2
	args.ExpectedScheduleVersion = updated.Version
	args.UserID = f.AmbassadorID
	_, err = f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
	require.ErrorIs(t, err, db.ErrConflict)
	args.UserID = f.OwnerID
	require.True(t, bindingChange(t, f, actor, args).Active)
	args.SpecialistID = "other"
	args.ExpectedVersion = 0
	second := bindingChange(t, f, actor, args)
	require.Equal(t, f.OwnerID, second.UserID)
}

func TestMassageBindingReceiptFailureRollsBackGrantAndSameOperationRetries(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, op, args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_binding_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ "+
			"BEGIN RAISE EXCEPTION 'synthetic binding receipt failure'; END $$; CREATE TRIGGER "+
			"fail_binding_receipt BEFORE UPDATE ON shuttle_massage_binding_operations FOR EACH "+
			"ROW WHEN (NEW.result IS NOT NULL) EXECUTE FUNCTION fail_binding_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.Error(t, err)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_specialist_bindings").Scan(&count),
	)
	require.Zero(t, count)
	_, err = f.H.store.MassageBindingOperation(t.Context(), actor, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER fail_binding_receipt ON shuttle_massage_binding_operations")
	require.NoError(t, err)
	result, err := f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		op,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Version)
}

func TestMassageBindingRevocationRemainsAvailableForRevokedTargetAndDisabledModule(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	grant := bindingChange(t, f, actor, args)
	_, err := f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", args.UserID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", args.EventID)
	require.NoError(t, err)
	args.Active = false
	args.ExpectedVersion = grant.Version
	require.False(t, bindingChange(t, f, actor, args).Active)
}

func TestMassageScheduleFailureRollsBackSpecialistRevocation(t *testing.T) {
	t.Parallel()
	f, actor, args, schedule := bindingFixture(t)
	other := schedule.Document.Specialists[0]
	other.ID = "other"
	schedule.Document.Specialists = append(schedule.Document.Specialists, other)
	schedule.ExpectedVersion = args.ExpectedScheduleVersion
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageSchedule(t.Context(), actor, op, schedule)
	require.NoError(t, err)
	updated, err := f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		op,
		prep.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	args.ExpectedScheduleVersion = updated.Version
	grant := bindingChange(t, f, actor, args)
	schedule.Document.Specialists = schedule.Document.Specialists[1:]
	schedule.ExpectedVersion = updated.Version
	op = uuid.New()
	prep, err = f.H.store.PrepareMassageSchedule(t.Context(), actor, op, schedule)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_binding_schedule() RETURNS trigger LANGUAGE plpgsql AS $$ "+
			"BEGIN RAISE EXCEPTION 'synthetic binding schedule failure'; END $$; CREATE "+
			"TRIGGER fail_binding_schedule BEFORE UPDATE ON shuttle_massage_schedules FOR EACH "+
			"ROW EXECUTE FUNCTION fail_binding_schedule()")
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		op,
		prep.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.Error(t, err)
	var active bool
	var version int64
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT active,version FROM shuttle_massage_specialist_bindings WHERE event_id=$1 "+
				"AND specialist_id='alex'", args.EventID).
			Scan(&active, &version),
	)
	require.True(t, active)
	require.Equal(t, grant.Version, version)
	current, err := f.H.store.MassageScheduleConfiguration(t.Context(), actor, args.EventID)
	require.NoError(t, err)
	require.Equal(t, updated, current)
	_, err = f.H.store.Pool().Exec(t.Context(), "DROP TRIGGER fail_binding_schedule ON shuttle_massage_schedules")
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		op,
		prep.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT active FROM shuttle_massage_specialist_bindings WHERE event_id=$1 AND "+
				"specialist_id='alex'", args.EventID).
			Scan(&active),
	)
	require.False(t, active)
}

func TestMassageBindingPreparedGrantRejectsRemovedSpecialist(t *testing.T) {
	t.Parallel()
	f, actor, args, schedule := bindingFixture(t)
	operation := uuid.New()
	grant, err := f.H.store.PrepareMassageBinding(t.Context(), actor, operation, args)
	require.NoError(t, err)
	other := schedule.Document.Specialists[0]
	other.ID = "other"
	schedule.Document.Specialists = []massage.ScheduleSpecialist{other}
	schedule.ExpectedVersion = args.ExpectedScheduleVersion
	update := uuid.New()
	change, err := f.H.store.PrepareMassageSchedule(t.Context(), actor, update, schedule)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		actor,
		update,
		change.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		operation,
		grant.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.MassageBindingOperation(t.Context(), actor, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_specialist_bindings").Scan(&count),
	)
	require.Zero(t, count)
}
