package integration_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedRegistrationOpaqueSourceLockOrder(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{knowledgeauthority.DerivedMemory, knowledgeauthority.DerivedProposal} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			checkOpaqueRegistrationLockOrder(t, kind)
		})
	}
}

func opaqueRegistrationSource(t *testing.T, db *pgxpool.Pool, kind, actor, event string) readsource.Derivation {
	t.Helper()
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{
		{Registration: passbooking.ReadAuthority{Kind: passbooking.ReadCapability, Event: event, Action: "solo"}},
	}}
	return storeOpaqueRegistrationSource(t, db, kind, actor, "opaque-registration", source)
}

func storeOpaqueRegistrationSource(
	t *testing.T, db *pgxpool.Pool, kind, actor, key string, source readsource.Derivation,
) readsource.Derivation {
	t.Helper()
	service := knowledge.Service{DB: db}
	command := knowledge.Command{
		Name:    knowledge.MemoSet,
		Key:     key,
		FactKey: key,
		Text:    "Registration context",
	}
	if kind == knowledgeauthority.DerivedProposal {
		command.Name, command.Topic = knowledge.Suggest, "registration"
	}
	result, err := service.ExecuteDerived(t.Context(), actor, command, source)
	require.NoError(t, err)
	if kind == knowledgeauthority.DerivedProposal {
		require.NotNil(t, result.Proposal)
		source.Authorities = result.Proposal.ReadAuthorities
	} else {
		memo, readErr := service.Memo(t.Context(), actor, command.FactKey)
		require.NoError(t, readErr)
		source.Authorities = memo.ReadAuthorities
	}
	require.Len(t, source.Authorities, 1)
	require.Equal(t, kind, source.Authorities[0].Knowledge.Kind)
	return source
}

func TestDerivedRegistrationOpaqueWindowKeepsPerRecordBudget(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{knowledgeauthority.DerivedMemory, knowledgeauthority.DerivedProposal} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, registration := bookingFixture(t)
			source := wideOpaqueRegistrationSource(t, db, kind)
			require.True(t, source.Valid(), "the current request contains only two opaque references")
			tx, err := db.Begin(t.Context())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
			valid, err := readsource.Lock(t.Context(), tx, "alice", source.Authorities)
			require.NoError(t, err)
			require.Equal(t, []bool{true, true}, valid, "ordinary source authorization accepts both bounded records")
			_, err = readsource.ExpandProposalSources(t.Context(), tx, source.Authorities)
			require.ErrorIs(t, err, readsource.ErrLimit, "the persisted-record merge limit must remain unchanged")
			require.NoError(t, tx.Rollback(t.Context()))
			service := derivedmutation.Service{DB: db, Registration: registration}
			command := passbooking.Command{Name: "solo", Event: "dance", Key: "wide-opaque-registration"}
			_, err = service.CapturePassAdmission(
				t.Context(),
				"alice",
				passbooking.AdmissionRequest{Command: command},
				source,
			)
			require.NoError(t, err, "registration prelocks must accept the same window as source authorization")
			_, err = service.ExecutePassBooking(t.Context(), "alice", command, source)
			require.NoError(t, err)
		})
	}
}

func wideOpaqueRegistrationSource(t *testing.T, db *pgxpool.Pool, kind string) readsource.Derivation {
	t.Helper()
	_, err := db.Exec(t.Context(), `WITH events AS (INSERT INTO core.pass_events(id,finishes_at)
 SELECT 'opaque-scope-'||i,now()+interval '30 days' FROM generate_series(0,$1::integer-1) i RETURNING id),
 scopes AS (INSERT INTO core.knowledge_scopes(scope,event_id) SELECT id,id FROM events RETURNING scope)
 INSERT INTO core.knowledge_permissions(scope,actor,permission)
 SELECT scope,'alice','review' FROM scopes`, readsource.MaxAuthorities)
	require.NoError(t, err)
	generation := int64(0)
	combined := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	for record := range 2 {
		source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{
			{Registration: passbooking.ReadAuthority{Kind: passbooking.ReadCapability, Event: "dance", Action: "solo"}},
		}}
		for leaf := range readsource.MaxAuthorities / 2 {
			scope := "opaque-scope-" + strconv.Itoa(record*readsource.MaxAuthorities/2+leaf)
			source.Authorities = append(source.Authorities, readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: scope},
			})
		}
		require.True(t, source.Valid())
		opaque := storeOpaqueRegistrationSource(t, db, kind, "alice", "opaque-window-"+strconv.Itoa(record), source)
		combined.Authorities = append(combined.Authorities, opaque.Authorities...)
	}
	return combined
}

func checkOpaqueRegistrationLockOrder(t *testing.T, kind string) {
	t.Helper()
	db, registration := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at) VALUES('aaa',now()+interval '30 days');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('aaa','bob');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('aaa',0,20,100,now()-interval '1 day')`)
	require.NoError(t, err)
	alice := opaqueRegistrationSource(t, db, kind, "alice", "aaa")
	bob := opaqueRegistrationSource(t, db, kind, "bob", "dance")
	service := derivedmutation.Service{DB: db, Registration: registration}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	barrier, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(context.WithoutCancel(ctx)) }()
	var pid int32
	require.NoError(t, barrier.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = barrier.Exec(ctx, `SELECT id FROM core.pass_events WHERE id='aaa' FOR UPDATE`)
	require.NoError(t, err)
	done := make(chan error, 2)
	for _, item := range []struct {
		actor, target string
		source        readsource.Derivation
	}{{"alice", "dance", alice}, {"bob", "aaa", bob}} {
		go func() {
			_, runErr := service.CapturePassAdmission(
				ctx,
				item.actor,
				passbooking.AdmissionRequest{
					Command: passbooking.Command{Name: "solo", Event: item.target, Key: "opaque-admission"},
				},
				item.source,
			)
			done <- runErr
		}()
	}
	waitMutationBlocked(t, db, pid, 2)
	probe, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = probe.Rollback(context.WithoutCancel(ctx)) }()
	_, err = probe.Exec(ctx, `SELECT id FROM core.pass_events WHERE id='dance' FOR UPDATE NOWAIT`)
	require.NoError(t, err, "opaque inherited events must join the sorted prelude")
	_, err = probe.Exec(
		ctx,
		`SELECT id FROM core.users WHERE id IN ('alice','bob') ORDER BY id FOR NO KEY UPDATE NOWAIT`,
	)
	require.NoError(t, err, "opaque source expansion must precede actor locks")
	require.NoError(t, probe.Rollback(ctx))
	require.NoError(t, barrier.Commit(ctx))
	require.NoError(t, <-done)
	require.NoError(t, <-done)
}
