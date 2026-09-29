package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func registrationDerivation(t *testing.T, db *pgxpool.Pool, actor string) readsource.Derivation {
	t.Helper()
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('',$1,'review')`,
		actor,
	)
	require.NoError(t, err)
	generation := int64(0)
	return readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
}

func TestDerivedRegistrationAndProfileRequireSourceForNewEffects(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"registration", "profile", "assignment"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, registration := bookingFixture(t)
			actor := "alice"
			if kind == "assignment" {
				actor = "bob"
				_, err := db.Exec(
					t.Context(),
					`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('dance','alice',1,'waitlist','leader','solo','bob',now())`,
				)
				require.NoError(t, err)
			}
			source := registrationDerivation(t, db, actor)
			service := derivedmutation.Service{DB: db, Registration: registration, Profile: passes.Service{DB: db}}
			call := func(key string) error {
				switch kind {
				case "registration":
					_, err := service.ExecutePassBooking(
						t.Context(),
						actor,
						bookingCommand("solo", key, passbooking.Booking{}),
						source,
					)
					return err
				case "profile":
					_, err := service.ExecutePassProfile(
						t.Context(),
						actor,
						passes.Command{
							Name:   "set",
							Field:  "legal_name",
							Value:  "Derived name",
							Origin: "agent",
							Key:    key,
						},
						source,
					)
					return err
				default:
					_, err := service.AssignPass(
						t.Context(),
						actor,
						passbooking.AdminAssignment{Event: "dance", Key: key, Target: "alice", TargetVersion: 1},
						source,
					)
					return err
				}
			}
			*source.Generation = 1
			requireCode(t, call("committed-source"), "history_stale")
			*source.Generation = 0
			require.NoError(t, call("committed-source"), "source rejection leaves no mutation or receipt")
			_, err := db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor=$1`, actor)
			require.NoError(t, err)
			require.NoError(t, call("committed-source"), "committed receipt survives source revocation")
			requireCode(t, call("new-source"), "source_stale")
			if kind == "assignment" {
				_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner=$1`, actor)
			} else {
				_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id=$1`, actor)
			}
			require.NoError(t, err)
			requireCode(t, call("committed-source"), "forbidden")
		})
	}
}

func TestDerivedRegistrationOrdersSourceAndTargetEventUnion(t *testing.T) {
	t.Parallel()
	db, registration := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at) VALUES('aaa',now()+interval '30 days');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('aaa','bob');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('aaa',0,20,100,now()-interval '1 day')`)
	require.NoError(t, err)
	service := derivedmutation.Service{DB: db, Registration: registration}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	barrier, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = barrier.Rollback(context.WithoutCancel(t.Context())) })
	var pid int32
	require.NoError(t, barrier.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = barrier.Exec(ctx, `SELECT id FROM core.pass_events WHERE id='aaa' FOR UPDATE`)
	require.NoError(t, err)
	done := make(chan error, 2)
	for _, item := range []struct{ actor, target, source string }{{"alice", "dance", "aaa"}, {"bob", "aaa", "dance"}} {
		go func() {
			generation := int64(0)
			source := readsource.Derivation{
				Generation: &generation,
				Authorities: []readsource.Authority{
					{
						Registration: passbooking.ReadAuthority{
							Kind:   passbooking.ReadCapability,
							Event:  item.source,
							Action: "solo",
						},
					},
				},
			}
			_, runErr := service.ExecutePassBooking(
				ctx,
				item.actor,
				passbooking.Command{Name: "solo", Event: item.target, Key: "opposed-event"},
				source,
			)
			done <- runErr
		}()
	}
	waitMutationBlocked(t, db, pid, 2)
	probe, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = probe.Rollback(context.WithoutCancel(t.Context())) })
	_, err = probe.Exec(ctx, `SELECT id FROM core.pass_events WHERE id='dance' FOR UPDATE NOWAIT`)
	require.NoError(t, err, "both unions must wait at their first sorted event")
	_, err = probe.Exec(
		ctx,
		`SELECT id FROM core.users WHERE id IN ('alice','bob') ORDER BY id FOR NO KEY UPDATE NOWAIT`,
	)
	require.NoError(t, err, "neither operation may hold actors before the event union")
	require.NoError(t, probe.Rollback(ctx))
	require.NoError(t, barrier.Commit(ctx))
	require.NoError(t, <-done)
	require.NoError(t, <-done)
}
