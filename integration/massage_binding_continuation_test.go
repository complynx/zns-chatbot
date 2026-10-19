package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMassageBindingNewOperationsContinueOneProposalAcrossInterfaces(t *testing.T) {
	t.Parallel()
	for _, nativeFirst := range []bool{true, false} {
		name := "agent_to_native"
		if nativeFirst {
			name = "native_to_agent"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, actor, args, _ := bindingFixture(t)
			session := nativeMassageSession(t, f, actor, 11001)
			original, adopted := uuid.New(), uuid.New()
			var prep db.MassageBindingPreparation
			var err error
			if nativeFirst {
				prep, err = f.H.store.PrepareMiniAppMassageBinding(t.Context(), actor, session, original, args)
			} else {
				prep, err = f.H.store.PrepareMassageBinding(t.Context(), actor, original, args)
			}
			require.NoError(t, err)
			var next db.MassageBindingPreparation
			if nativeFirst {
				next, err = f.H.store.AdoptMassageBinding(t.Context(), actor, adopted, prep.ID, args)
			} else {
				next, err = f.H.store.AdoptMiniAppMassageBinding(t.Context(), actor, session, adopted, prep.ID, args)
			}
			require.NoError(t, err)
			require.Equal(t, prep, next)
			pending, err := f.H.store.PendingMassageBindings(t.Context(), actor, &session, uuid.Nil, uuid.Nil)
			require.NoError(t, err)
			require.Len(t, pending.Proposals, 1)
			require.Equal(t, original, pending.Proposals[0].OperationID)
			type outcome struct {
				value db.MassageSpecialistBinding
				err   error
			}
			done := make(chan outcome, 2)
			// Both interfaces may race after consent. Event serialization must yield one effect.
			go func() {
				v, e := f.H.store.ChangeMassageBinding(
					t.Context(),
					actor,
					original,
					prep.ID,
					args,
					func(context.Context) error { return nil },
				)
				done <- outcome{v, e}
			}()
			go func() {
				v, e := f.H.store.ChangeMiniAppMassageBinding(
					t.Context(),
					actor,
					session,
					adopted,
					prep.ID,
					args,
					func(context.Context) error { return nil },
				)
				done <- outcome{v, e}
			}()
			first, second := <-done, <-done
			require.NoError(t, first.err)
			require.NoError(t, second.err)
			require.Equal(t, first.value, second.value)
			require.Equal(t, int64(1), first.value.Version)
			revoke := args
			revoke.Active = false
			revoke.ExpectedVersion = 1
			require.False(t, bindingChange(t, f, actor, revoke).Active)
			_, err = f.H.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET expires_at=clock_timestamp()-interval '1 second' WHERE actor_id=$1 AND operation_id=$2", actor.UserID, original)
			require.NoError(t, err)
			for _, op := range []uuid.UUID{original, adopted} {
				receipt, e := f.H.store.ChangeMassageBinding(
					t.Context(),
					actor,
					op,
					prep.ID,
					args,
					func(context.Context) error { return nil },
				)
				require.NoError(t, e)
				require.Equal(t, first.value, receipt)
				status, e := f.H.store.MassageBindingStatus(t.Context(), actor, &session, op, uuid.Nil)
				require.NoError(t, e)
				require.Equal(t, "completed", status.State)
				require.Equal(t, original, status.OperationID)
				require.Equal(t, first.value, *status.Result)
			}
			status, err := f.H.store.MassageBindingStatus(t.Context(), actor, nil, uuid.Nil, prep.ID)
			require.NoError(t, err)
			require.Equal(t, first.value, *status.Result)
			pending, err = f.H.store.PendingMassageBindings(t.Context(), actor, nil, uuid.Nil, uuid.Nil)
			require.NoError(t, err)
			require.Empty(t, pending.Proposals)
			_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, uuid.New(), prep.ID, args)
			require.ErrorIs(t, err, db.ErrConflict)
		})
	}
}

func TestMassageBindingAdoptionRequiresExactActorProposalAndPendingArguments(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	original := uuid.New()
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, original, args)
	require.NoError(t, err)
	alias := uuid.New()
	adopted, err := f.H.store.AdoptMassageBinding(t.Context(), actor, alias, prep.ID, args)
	require.NoError(t, err)
	require.Equal(t, prep, adopted)
	second, err := f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
	require.NoError(t, err)
	_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, alias, second.ID, args)
	require.ErrorIs(t, err, db.ErrConflict)
	changed := args
	changed.Active = false
	_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, uuid.New(), prep.ID, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	other := ownerProfileScope(t, f.H, f.OwnerID)
	_, err = f.H.store.AdoptMassageBinding(t.Context(), other, uuid.New(), prep.ID, args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.MassageBindingStatus(t.Context(), other, nil, uuid.Nil, prep.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	pending, err := f.H.store.PendingMassageBindings(t.Context(), other, nil, uuid.Nil, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, pending.Proposals)
	pending, err = f.H.store.PendingMassageBindings(t.Context(), actor, nil, f.Event.ID, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, pending.Proposals, 2)
	_, err = f.H.store.MassageBindingStatus(t.Context(), actor, nil, original, prep.ID)
	require.Error(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET expires_at=clock_timestamp()-interval '1 second'")
	require.NoError(t, err)
	status, err := f.H.store.MassageBindingStatus(t.Context(), actor, nil, alias, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, "expired", status.State)
	_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, uuid.New(), prep.ID, args)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		alias,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrConflict)
	pending, err = f.H.store.PendingMassageBindings(t.Context(), actor, nil, uuid.Nil, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, pending.Proposals)
}

func TestMassageBindingAdoptionFailureAndAuthorizationLeaveNoEffect(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, op, args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `CREATE FUNCTION fail_binding_alias() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic alias failure'; END $$;
 CREATE TRIGGER fail_binding_alias BEFORE INSERT ON shuttle_massage_binding_operation_aliases FOR EACH ROW EXECUTE FUNCTION fail_binding_alias()`)
	require.NoError(t, err)
	alias := uuid.New()
	_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, alias, prep.ID, args)
	require.Error(t, err)
	_, err = f.H.store.MassageBindingStatus(t.Context(), actor, nil, alias, uuid.Nil)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER fail_binding_alias ON shuttle_massage_binding_operation_aliases")
	require.NoError(t, err)
	_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, alias, prep.ID, args)
	require.NoError(t, err)
	denied := errors.New("administrator revoked")
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		alias,
		prep.ID,
		args,
		func(context.Context) error { return denied },
	)
	require.ErrorIs(t, err, denied)
	_, err = f.H.store.MassageBinding(t.Context(), actor, nil, args.EventID, args.SpecialistID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	status, err := f.H.store.MassageBindingStatus(t.Context(), actor, nil, alias, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, "pending", status.State)
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		alias,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	session := nativeMassageSession(t, f, actor, 11001)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1", actor.UserID)
	require.NoError(t, err)
	_, err = f.H.store.MassageBindingStatus(t.Context(), actor, &session, alias, uuid.Nil)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.PendingMassageBindings(t.Context(), actor, &session, uuid.Nil, uuid.Nil)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.AdoptMiniAppMassageBinding(t.Context(), actor, session, uuid.New(), prep.ID, args)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}

func TestMassageBindingPendingPaginationReachesEveryCanonicalProposal(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	seen := map[uuid.UUID]bool{}
	for range 21 {
		p, err := f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
		require.NoError(t, err)
		seen[p.ID] = false
	}
	first, err := f.H.store.PendingMassageBindings(t.Context(), actor, nil, uuid.Nil, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, first.Proposals, 20)
	require.NotEmpty(t, first.NextAfter)
	cursor, err := uuid.Parse(first.NextAfter)
	require.NoError(t, err)
	// The continuation remains usable after the cursor proposal itself expires.
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", cursor)
	require.NoError(t, err)
	second, err := f.H.store.PendingMassageBindings(t.Context(), actor, nil, uuid.Nil, cursor)
	require.NoError(t, err)
	require.Len(t, second.Proposals, 1)
	require.Empty(t, second.NextAfter)
	for _, page := range []db.MassageBindingPendingPage{first, second} {
		for _, p := range page.Proposals {
			known := seen[p.ID]
			require.False(t, known)
			seen[p.ID] = true
		}
	}
	for _, found := range seen {
		require.True(t, found)
	}
	other := ownerProfileScope(t, f.H, f.OwnerID)
	_, err = f.H.store.PendingMassageBindings(t.Context(), other, nil, uuid.Nil, cursor)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
