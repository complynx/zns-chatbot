package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type registrationTurn struct {
	ID       int64
	Ingress  int64
	Position int64
	Count    int64
	Deadline time.Time
	State    string
}

func readRegistrationTurn(t *testing.T, db *pgxpool.Pool, owner string) registrationTurn {
	t.Helper()
	var value registrationTurn
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT id,ingress_id,effective_position,requeue_count,turn_expires_at,state FROM core.registration_intents WHERE event_id='dance' AND owner=$1 AND state<>'cancelled'`, owner).
			Scan(&value.ID, &value.Ingress, &value.Position, &value.Count, &value.Deadline, &value.State),
	)
	return value
}

func expireAliceRegistrationTurn(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.registration_intents SET turn_expires_at=clock_timestamp()-interval '1 second' WHERE event_id='dance' AND owner=$1 AND state='captured'`,
		"alice",
	)
	require.NoError(t, err)
}

func TestRegistrationRetentionRequeuesWithoutDeletingDraft(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.RegistrationRetention = 3 * time.Minute
	ctx := t.Context()
	command := bookingCommand("solo", "retention-first", passbooking.Booking{})
	admission, err := s.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{Command: command})
	require.NoError(t, err)
	before := readRegistrationTurn(t, db, "alice")
	require.Equal(t, admission.Position, before.Position)
	_, err = s.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{Command: command})
	require.NoError(t, err)
	require.Equal(t, before, readRegistrationTurn(t, db, "alice"), "repeat cannot renew a turn")
	bobCommand := bookingCommand("solo", "later-complete", passbooking.Booking{})
	bob, err := s.Execute(ctx, "bob", bobCommand)
	require.NoError(t, err)
	require.Equal(t, "waitlist", bob.State, "earlier unfinished application owns the turn")
	var announcements int
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT count(*) FROM core.pass_registration_announcements`).Scan(&announcements),
	)
	require.Zero(t, announcements, "later completion cannot announce before unfinished head")
	expireAliceRegistrationTurn(t, db)
	restarted := passbooking.Service{DB: db, Delivery: s.Delivery, RegistrationRetention: 3 * time.Minute}
	_, err = restarted.ProcessDeadlines(ctx)
	require.NoError(t, err)
	after := readRegistrationTurn(t, db, "alice")
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.Ingress, after.Ingress)
	require.Equal(t, "captured", after.State)
	require.EqualValues(t, 1, after.Count)
	require.Greater(t, after.Position, readRegistrationTurn(t, db, "bob").Position)
	require.True(t, after.Deadline.After(before.Deadline.Add(-time.Minute)))
	var role string
	require.NoError(t, db.QueryRow(ctx, `SELECT role FROM core.pass_profiles WHERE owner='alice'`).Scan(&role))
	require.Equal(t, "leader", role, "draft profile remains intact")
	bob, err = restarted.Get(ctx, "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, "assigned", bob.State)
	var published []string
	rows, err := db.Query(ctx, `SELECT owner FROM core.pass_registration_announcements ORDER BY id`)
	require.NoError(t, err)
	for rows.Next() {
		var owner string
		require.NoError(t, rows.Scan(&owner))
		published = append(published, owner)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []string{"bob"}, published)
	_, err = restarted.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{Command: command})
	require.NoError(t, err)
	require.Equal(t, after, readRegistrationTurn(t, db, "alice"), "restart/repeat does not move the tail again")
	alice, err := restarted.Execute(ctx, "alice", command)
	require.NoError(t, err)
	require.NotEqual(t, "cancelled", alice.State)
	completed := readRegistrationTurn(t, db, "alice")
	require.Equal(t, "registered", completed.State)
	_, err = db.Exec(
		ctx,
		`UPDATE core.registration_intents SET turn_expires_at=clock_timestamp()-interval '1 second' WHERE event_id='dance'`,
	)
	require.NoError(t, err)
	_, err = restarted.ProcessDeadlines(ctx)
	require.NoError(t, err)
	require.Equal(t, completed.Position, readRegistrationTurn(t, db, "alice").Position)
	currentBob, err := restarted.Get(ctx, "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, bob, currentBob, "completed assigned booking is not cancelled or moved")
}

func TestRegistrationRetentionPreopenRepeatAndRestart(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	ctx := t.Context()
	_, err := db.Exec(
		ctx,
		`UPDATE core.pass_event_tiers SET starts_at=clock_timestamp()+interval '1 day' WHERE event_id='dance'`,
	)
	require.NoError(t, err)
	command := bookingCommand("solo", "preopen-retention", passbooking.Booking{})
	_, err = s.Execute(ctx, "alice", command)
	requireCode(t, err, "pass_sales_closed")
	original := readRegistrationTurn(t, db, "alice")
	_, err = s.Execute(ctx, "alice", command)
	requireCode(t, err, "pass_sales_closed")
	require.Equal(t, original, readRegistrationTurn(t, db, "alice"))
	expireAliceRegistrationTurn(t, db)
	s = passbooking.Service{DB: db, Delivery: s.Delivery}
	_, err = s.ProcessDeadlines(ctx)
	require.NoError(t, err)
	rotated := readRegistrationTurn(t, db, "alice")
	require.Equal(t, original.ID, rotated.ID)
	require.Equal(t, original.Ingress, rotated.Ingress)
	require.Greater(t, rotated.Position, original.Position)
	require.Equal(t, "captured", rotated.State)
	var bookings int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.pass_bookings`).Scan(&bookings))
	require.Zero(t, bookings)
	_, err = db.Exec(
		ctx,
		`UPDATE core.pass_event_tiers SET starts_at=clock_timestamp()-interval '1 day' WHERE event_id='dance'`,
	)
	require.NoError(t, err)
	booked, err := s.Execute(ctx, "alice", command)
	require.NoError(t, err)
	require.Equal(t, "assigned", booked.State)
	require.Equal(t, rotated.Position, readRegistrationTurn(t, db, "alice").Position)
}

func TestRegistrationRetentionLateIngressCannotUndoTail(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	f.b.Delivery.BotID = 99078
	handle(t, f.b, message(82901, 101, "/passes"))
	handle(t, f.b, message(82902, 101, "/passes"))
	command := bookingCommand("solo", "retained-key", passbooking.Booking{})
	later := registrationingress.WithReference(
		t.Context(),
		registrationingress.Reference{BotID: 99078, UpdateID: 82902},
	)
	_, err := f.b.Host.AdmitPassBooking(later, "alice", command, nil)
	require.NoError(t, err)
	original := readRegistrationTurn(t, f.db, "alice")
	expireAliceRegistrationTurn(t, f.db)
	service := passbooking.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	moved := readRegistrationTurn(t, f.db, "alice")
	earlier := registrationingress.WithReference(
		t.Context(),
		registrationingress.Reference{BotID: 99078, UpdateID: 82901},
	)
	_, err = f.b.Host.AdmitPassBooking(earlier, "alice", command, nil)
	require.NoError(t, err)
	replay := readRegistrationTurn(t, f.db, "alice")
	require.Less(t, replay.Ingress, original.Ingress, "audit can recover the earlier original input")
	require.Equal(t, moved.Position, replay.Position, "an old input cannot restore queue priority")
	require.Equal(t, moved.Deadline, replay.Deadline)
	require.Equal(t, moved.Count, replay.Count)
}

type retainedIngressBarrier struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *retainedIngressBarrier) Start(ctx context.Context, name string) (context.Context, func(error)) {
	if name == "telegram.update" {
		b.once.Do(func() {
			close(b.entered)
			select {
			case <-b.release:
			case <-ctx.Done():
			}
		})
	}
	return ctx, func(error) {}
}

func TestRegistrationRetentionPollerCannotOvertakeDelayedEarlierInput(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	for index, owner := range []string{"alice", "bob"} {
		command := bookingCommand("solo", "poller-registration-"+owner, passbooking.Booking{})
		plan := interaction.SavedPlan{
			Plan:                agent.Plan{View: agent.RegistrationView},
			RegistrationCommand: &command,
			PassAuthority: &interaction.PlanAuthority{
				Reads:           []interaction.PassContextDependency{},
				ReadAuthorities: []readsource.Authority{},
			},
		}
		plan.BindKind()
		_, err := (interaction.Store{DB: f.db}).SaveWinner(t.Context(), owner, int64(index+1), plan)
		require.NoError(t, err)
	}
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "Register me for Dance"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 202, "text": "Register me for Dance"})
	barrier := &retainedIngressBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	f.b.Observer = barrier
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan error, 1)
	go func() { ended <- f.b.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Error("poller did not join")
		}
	})
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("earlier update did not reach barrier")
	}
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&count))
	require.Equal(t, 2, count, "both inputs must be durably received before delayed processing")
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.registration_ingress`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings`).Scan(&count))
	require.Zero(t, count, "later input cannot allocate while earlier event-specific processing is delayed")
	close(barrier.release)
	require.Eventually(t, func() bool {
		err := f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.registration_intents WHERE state='registered'`).
			Scan(&count)
		return err == nil && count == 2
	}, 5*time.Second, 10*time.Millisecond)
	first := readRegistrationTurn(t, f.db, "alice")
	second := readRegistrationTurn(t, f.db, "bob")
	require.Less(t, first.Ingress, second.Ingress)
	require.Less(t, first.Position, second.Position)
	var firstHype, secondHype int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.pass_registration_announcements WHERE owner='alice'`).
			Scan(&firstHype),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.pass_registration_announcements WHERE owner='bob'`).
			Scan(&secondHype),
	)
	require.Less(t, firstHype, secondHype)
}

func TestRegistrationRetentionExactDeadlineFence(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	ctx := t.Context()
	command := bookingCommand("solo", "exact-expiry", passbooking.Booking{})
	_, err := s.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{Command: command})
	require.NoError(t, err)
	before := readRegistrationTurn(t, db, "alice")
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT id FROM core.pass_events WHERE id='dance' FOR UPDATE`)
	require.NoError(t, err)
	q := dbgen.New(tx)
	observed := pgtype.Timestamptz{Time: before.Deadline.Add(-time.Microsecond), Valid: true}
	ids, err := q.ExpiredRegistrationTurns(
		ctx,
		dbgen.ExpiredRegistrationTurnsParams{EventID: "dance", ObservedAt: observed},
	)
	require.NoError(t, err)
	require.Empty(t, ids)
	observed.Time = before.Deadline
	ids, err = q.ExpiredRegistrationTurns(
		ctx,
		dbgen.ExpiredRegistrationTurnsParams{EventID: "dance", ObservedAt: observed},
	)
	require.NoError(t, err)
	require.Equal(t, []int64{before.ID}, ids)
	deadline := pgtype.Timestamptz{Time: observed.Time.Add(10 * time.Minute), Valid: true}
	request := dbgen.RotateRegistrationTurnParams{ID: before.ID, Deadline: deadline, ObservedAt: observed}
	require.NoError(t, q.RotateRegistrationTurn(ctx, request))
	require.NoError(t, q.RotateRegistrationTurn(ctx, request), "same expired-turn retry must have no effect")
	require.NoError(t, tx.Commit(ctx))
	after := readRegistrationTurn(t, db, "alice")
	require.EqualValues(t, 1, after.Count)
	require.Equal(t, deadline.Time, after.Deadline)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.Ingress, after.Ingress)
}
