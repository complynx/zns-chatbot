package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedPassBatchResumesWithStoredSource(t *testing.T) {
	t.Parallel()
	db, registration := adminPairFixture(t)
	service := derivedmutation.Service{DB: db, Registration: registration}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review');
 CREATE FUNCTION core.interrupt_second_batch_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.plan->'items'->1->'outcome'->>'status' <> 'not_attempted' THEN RAISE EXCEPTION 'synthetic second marker interruption'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER interrupt_second_batch_item BEFORE UPDATE ON core.pass_admin_batches FOR EACH ROW EXECUTE FUNCTION core.interrupt_second_batch_item()`,
	)
	require.NoError(t, err)
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
	tier := 1
	command := passbooking.RuntimeBatch{
		Key:        "derived-resume",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101, 202},
		Options:    passbooking.AdminAssignment{AppendTier: &tier},
	}
	_, err = service.RunPassBatch(t.Context(), "bob", command, source)
	require.ErrorContains(t, err, "synthetic second marker interruption")
	var first, second string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT plan->'items'->0->'outcome'->>'status',plan->'items'->1->'outcome'->>'status' FROM core.pass_admin_batches`).
			Scan(&first, &second),
	)
	require.Equal(t, string(passbooking.AdminBatchSucceeded), first)
	require.Equal(t, string(passbooking.AdminBatchNotAttempted), second)
	_, err = db.Exec(
		t.Context(),
		`DROP TRIGGER interrupt_second_batch_item ON core.pass_admin_batches; DELETE FROM core.knowledge_permissions WHERE actor='bob'`,
	)
	require.NoError(t, err)
	_, err = registration.RunBatch(t.Context(), "bob", command)
	requireCode(t, err, "derived_batch_requires_coordinator")
	items, err := service.RunManualPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	require.Equal(t, passbooking.AdminBatchRejected, items[1].Outcome.Status)
	require.Equal(t, "source_stale", items[1].Outcome.Code)
	var amount int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	require.Equal(t, 22, amount, "only the committed first assignment changes capacity")
	// A new host generation and empty references cannot replace the persisted
	// evidence; both terminal outcomes remain truthful and stable.
	generation = 99
	source.Authorities = []readsource.Authority{}
	replay, err := service.RunPassBatch(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.Equal(t, items, replay)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.RunManualPassBatch(t.Context(), "bob", command)
	requireCode(t, err, "forbidden")
}

func TestManualPassBatchCoordinatorDoesNotInventSource(t *testing.T) {
	t.Parallel()
	db, registration := adminPairFixture(t)
	service := derivedmutation.Service{DB: db, Registration: registration}
	command := passbooking.RuntimeBatch{
		Key:        "manual-source-free",
		Event:      "dance",
		Action:     "admin_cancel",
		Recipients: []int64{101},
	}
	items, err := service.RunManualPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	var manual bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_derivation IS NULL FROM core.pass_admin_batches`).Scan(&manual),
	)
	require.True(t, manual)
	generation := int64(100)
	replay, err := service.RunPassBatch(
		t.Context(),
		"bob",
		command,
		readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
	)
	require.NoError(t, err)
	require.Equal(t, items, replay)
}

func TestDerivedPassBatchCommittedReceiptPrecedesSourceFence(t *testing.T) {
	t.Parallel()
	db, registration := adminPairFixture(t)
	service := derivedmutation.Service{DB: db, Registration: registration}
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
	tier := 1
	command := passbooking.RuntimeBatch{
		Key:        "receipt-recovery",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101},
		Options:    passbooking.AdminAssignment{AppendTier: &tier},
	}
	_, err := service.RunPassBatch(t.Context(), "bob", command, source)
	requireCode(t, err, "source_stale")
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_batches`).Scan(&count))
	require.Zero(t, count, "invalid source cannot create a grounded batch")
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	items, err := service.RunPassBatch(t.Context(), "bob", command, source)
	require.NoError(t, err)
	// Exercise recovery from a pending marker backed by an existing committed
	// domain receipt. The actual effect and immutable source remain untouched.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_admin_batches SET plan=jsonb_set(plan,'{items,0,outcome,status}','"not_attempted"'); DELETE FROM core.knowledge_permissions WHERE actor='bob'`,
	)
	require.NoError(t, err)
	replay, err := service.RunManualPassBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Equal(t, items, replay)
	var amount int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	require.Equal(t, 22, amount)
}
