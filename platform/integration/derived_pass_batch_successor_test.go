package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func successorBatchFixture(
	t *testing.T,
) (*pgxpool.Pool, derivedmutation.Service, passbooking.RuntimeBatch, readsource.Derivation) {
	t.Helper()
	db, registration := adminPairFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='visitor';
INSERT INTO core.pass_booking_admins(owner) VALUES('visitor');
UPDATE core.pass_bookings SET partner='',kind='solo',state='assigned',assigned_at=now(),price=100 WHERE event_id='dance'`)
	require.NoError(t, err)
	read, err := registration.AdminTarget(t.Context(), "visitor", "dance", 101)
	require.NoError(t, err)
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, PrivateHistory: true,
		Authorities: readsource.Registration([]passbooking.ReadAuthority{{Kind: passbooking.ReadPrivileged,
			Event: "dance", Owner: read.Booking.Owner, Version: read.Booking.Version, CreatedAt: read.Booking.CreatedAt,
			Action: "admin_assign", TargetTelegramID: 101}})}
	price := 3141
	command := passbooking.RuntimeBatch{Key: "receipt-successor", Event: "dance", Action: "admin_assign",
		Recipients: []int64{101, 202}, Options: passbooking.AdminAssignment{TotalPrice: &price}}
	return db, derivedmutation.Service{DB: db, Registration: registration}, command, source
}

func interruptSuccessorBatch(
	t *testing.T,
	service derivedmutation.Service,
	command passbooking.RuntimeBatch,
	source readsource.Derivation,
) {
	t.Helper()
	_, err := service.DB.Exec(
		t.Context(),
		`CREATE FUNCTION core.pause_successor() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.plan->'items'->1->'outcome'->>'status' <> 'not_attempted' THEN
RAISE EXCEPTION 'synthetic successor interruption'; END IF; RETURN NEW; END $$;
CREATE TRIGGER pause_successor BEFORE UPDATE ON core.pass_admin_batches FOR EACH ROW EXECUTE FUNCTION core.pause_successor()`,
	)
	require.NoError(t, err)
	_, err = service.RunPassBatch(t.Context(), "visitor", command, source)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "synthetic successor interruption")
	var first, second string
	require.NoError(
		t,
		service.DB.QueryRow(t.Context(), `SELECT plan->'items'->0->'outcome'->>'status',plan->'items'->1->'outcome'->>'status' FROM core.pass_admin_batches`).
			Scan(&first, &second),
	)
	require.Equal(t, "succeeded", first)
	require.Equal(t, "not_attempted", second)
	_, err = service.DB.Exec(t.Context(), `DROP TRIGGER pause_successor ON core.pass_admin_batches`)
	require.NoError(t, err)
}

func TestDerivedPassBatchSuccessorResume(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"resume", "external_batch", "grant", "target_grant", "recreated_identity", "knowledge", "history", "missing_receipt", "request_hash", "foreign_causal"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, service, command, source := successorBatchFixture(t)
			_, err := db.Exec(
				t.Context(),
				`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','visitor','review')`,
			)
			require.NoError(t, err)
			causalActor := "visitor"
			if scenario == "foreign_causal" {
				causalActor = "bob"
			}
			source.Authorities = append(source.Authorities, readsource.Authority{Causal: &readsource.CausalSource{
				Actor: causalActor, Generation: source.Generation, PrivateHistory: causalActor == "visitor",
				Authorities: readsource.CloneAuthorities(source.Authorities),
			}}, readsource.Authority{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}})
			history := conversation.Service{DB: db}
			require.NoError(
				t,
				history.AppendOriginal(t.Context(), "visitor", "successor-original", "user", "synthetic batch source"),
			)
			interruptSuccessorBatch(t, service, command, source)
			changeSuccessorState(t, service, command, scenario)
			// New service instance and unrelated caller source cannot replace stored evidence.
			resumed := derivedmutation.Service{DB: db, Registration: passbooking.Service{DB: db}}
			items, err := resumed.RunManualPassBatch(t.Context(), "visitor", command)
			if scenario == "grant" {
				requireCode(t, err, "forbidden")
				assertSuccessorReceipts(t, db, command, scenario)
				return
			}
			require.NoError(t, err)
			require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
			if scenario == "resume" {
				require.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
			} else {
				require.Equal(t, passbooking.AdminBatchRejected, items[1].Outcome.Status)
				// The causal-origin generation is checked inside the source fence,
				// before the top-level history fence, including explicit deletion.
				require.Equal(t, "source_stale", items[1].Outcome.Code)
			}
			assertSuccessorReceipts(t, db, command, scenario)
		})
	}
}

func changeSuccessorState(
	t *testing.T,
	service derivedmutation.Service,
	command passbooking.RuntimeBatch,
	scenario string,
) {
	t.Helper()
	var err error
	switch scenario {
	case "external_batch":
		command.Key, command.Recipients = "unrelated-batch", []int64{101}
		_, err = service.RunManualPassBatch(t.Context(), "visitor", command)
		require.NoError(t, err)
		current, readErr := service.Registration.Get(t.Context(), "alice", "dance")
		require.NoError(t, readErr)
		require.Equal(t, int64(3), current.Version, "same-value external assignment still advances the version")
		require.Equal(t, 3141, *current.Price)
	case "grant":
		_, err = service.DB.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='visitor'`)
	case "target_grant":
		_, err = service.DB.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	case "recreated_identity":
		_, err = service.DB.Exec(
			t.Context(),
			`UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE event_id='dance' AND owner='alice'`,
		)
	case "knowledge":
		_, err = service.DB.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='visitor'`)
	case "history":
		var id int64
		err = service.DB.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='visitor' AND source_key='successor-original'`).
			Scan(&id)
		require.NoError(t, err)
		err = (conversation.Service{DB: service.DB}).DeleteContent(t.Context(), "visitor", id)
	case "missing_receipt":
		_, err = service.DB.Exec(t.Context(), `DELETE FROM core.pass_admin_assignments WHERE actor='visitor'`)
	case "request_hash":
		_, err = service.DB.Exec(
			t.Context(),
			`UPDATE core.pass_booking_operations SET request_hash=repeat('0',64) WHERE actor='visitor'`,
		)
	}
	require.NoError(t, err)
}

func assertSuccessorReceipts(t *testing.T, db *pgxpool.Pool, command passbooking.RuntimeBatch, scenario string) {
	t.Helper()
	var second int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments WHERE actor='visitor' AND target='bob'`).
			Scan(&second),
	)
	if scenario == "resume" {
		require.Equal(t, 1, second)
	} else {
		require.Zero(t, second)
	}
	var source []byte
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_derivation FROM core.pass_admin_batches WHERE actor='visitor' AND plan->'items'->0->'assignment'->>'key' LIKE 'batch-%' AND plan->'items'->1 IS NOT NULL`).
			Scan(&source),
	)
	var original readsource.Derivation
	require.NoError(t, json.Unmarshal(source, &original))
	require.Equal(t, int64(1), original.Authorities[0].Registration.Version)
	if scenario == "resume" {
		items, err := (derivedmutation.Service{DB: db, Registration: passbooking.Service{DB: db}}).RunManualPassBatch(
			t.Context(),
			"visitor",
			command,
		)
		require.NoError(t, err)
		require.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
	}
}

func TestDerivedPassBatchReceiptSuccessor(t *testing.T) {
	t.Parallel()
	db, service, command, source := successorBatchFixture(t)
	original, err := json.Marshal(source)
	require.NoError(t, err)
	items, err := service.RunPassBatch(t.Context(), "visitor", command, source)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	require.Equal(
		t,
		passbooking.AdminBatchSucceeded,
		items[1].Outcome.Status,
		"own first effect must not invalidate the batch",
	)
	var stored []byte
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_derivation FROM core.pass_admin_batches WHERE actor='visitor'`).
			Scan(&stored),
	)
	require.JSONEq(t, string(original), string(stored))
	after, err := json.Marshal(source)
	require.NoError(t, err)
	require.Equal(t, original, after)
	var receipts int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments WHERE actor='visitor'`).
			Scan(&receipts),
	)
	require.Equal(t, 2, receipts)
}

func TestDerivedPassBatchPartnerSuccessor(t *testing.T) {
	t.Parallel()
	db, service, command, source := successorBatchFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET kind='couple',partner=CASE owner WHEN 'alice' THEN 'bob' ELSE 'alice' END WHERE event_id='dance';
INSERT INTO core.users(id,telegram_id,name,can_book) VALUES('successor-third',404,'Synthetic third recipient',true);
INSERT INTO core.pass_profiles(owner,role) VALUES('successor-third','leader');
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
VALUES('dance','successor-third',1,'assigned','leader','solo','bob',now(),now(),100)`,
	)
	require.NoError(t, err)
	read, err := service.Registration.AdminTarget(t.Context(), "visitor", "dance", 202)
	require.NoError(t, err)
	source.Authorities[0].Registration = passbooking.ReadAuthority{Kind: passbooking.ReadPrivileged,
		Event: "dance", Owner: read.Booking.Owner, Version: read.Booking.Version, CreatedAt: read.Booking.CreatedAt,
		Action: "admin_assign", TargetTelegramID: 202}
	command.Recipients = []int64{101, 404}
	items, err := service.RunPassBatch(t.Context(), "visitor", command, source)
	require.NoError(t, err)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	require.Len(t, items[0].Outcome.Assignment.Bookings, 2, "first receipt includes the affected partner")
	require.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
}
