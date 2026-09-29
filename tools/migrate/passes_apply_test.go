package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type passImportOptions struct {
	announcementChannel                                                                                                                            bool
	unavailable, historical, divergentActors, reviewed, secondOnly, staleReview, foodMarkers, assignmentTier, distinctAssignments, mixedGeneration bool
	announcement                                                                                                                                   string
}

func passImportInputs(t *testing.T, unavailable, historical bool) (string, string, string) {
	t.Helper()
	return passImportFixture(t, passImportOptions{unavailable: unavailable, historical: historical})
}
func passImportFixture(t *testing.T, options passImportOptions) (string, string, string) {
	t.Helper()
	unavailable, historical := options.unavailable, options.historical
	dir, m := snapshot(t)
	m.Files = nil
	free := `{"state":"paid","role":"leader","type":"guest","date_created":"2026-09-01T00:00:00Z","date_assignment":"2026-09-02T00:00:00Z","price":0,"proof_admin":101,"proof_admin_received":101,"proof_received":"2026-09-02T00:00:00Z","proof_accepted":"2026-09-02T00:00:00Z","proof_file":"free_pass"}`
	if options.announcement != "" {
		free = strings.TrimSuffix(free, "}") + `,"sent_to_hype_thread":` + options.announcement + `}`
	}
	if options.foodMarkers {
		free = strings.TrimSuffix(free, "}") + `,"notified_food_first":true}`
	}
	users := `{"_id":"u101","bot_id":77,"user_id":101,"print_name":"Synthetic A","proof_admins":{"event_one":101},"notified_passport_data_required":false}` + "\n" +
		`{"_id":"u202","bot_id":77,"user_id":202,"print_name":"Synthetic B"}` + "\n" +
		`{"_id":"u303","bot_id":77,"user_id":303,"print_name":"Synthetic C","event_one":` + free + `}` + "\n"
	addFile(t, dir, &m, "users.jsonl", "users", "records", []byte(users), 3)
	event := strings.TrimSuffix(strings.TrimSpace(syntheticEvent), "}") + `,"payment_admin":[101]}` + "\n"
	if options.announcementChannel {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(event), &fields))
		fields["thread_channel"] = json.RawMessage(`"synthetic"`)
		fields["finish_date"] = json.RawMessage(`null`)
		raw, err := json.Marshal(fields)
		require.NoError(t, err)
		event = string(raw) + "\n"
	}
	addFile(t, dir, &m, "events.jsonl", "events", "records", []byte(event), 1)
	first := `{"_id":"p101","bot_id":77,"pass_key":"event_one","user_id":101,"state":"paid","role":"leader","type":"custom-pair","couple":202,"date_created":"2026-09-01T00:00:00Z","date_assignment":"2026-09-02T00:00:00Z","price":100,"proof_admin":101,"proof_received":"2026-09-03T00:00:00Z","proof_file":"file.jpg","skip_in_balance_count":false,"pass_type_index":0,"notified_deadline_close":"2026-09-02T12:00:00Z"}`
	if historical {
		first = strings.ReplaceAll(first, `"state":"paid"`, `"state":"assigned"`)
		first = strings.ReplaceAll(
			first,
			`"date_assignment":"2026-09-02T00:00:00Z"`,
			`"date_assignment":"2026-09-05T00:00:00Z"`,
		)
	}
	second := strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(strings.ReplaceAll(first, `"p101"`, `"p202"`), `"user_id":101`, `"user_id":202`),
			`"couple":202`,
			`"couple":101`,
		),
		`"role":"leader"`,
		`"role":"follower"`,
	)
	first, second = passImportRecordOptions(first, second, options)
	if options.announcement != "" {
		first = strings.TrimSuffix(first, "}") + `,"sent_to_hype_thread":` + options.announcement + `}`
		second = strings.TrimSuffix(second, "}") + `,"sent_to_hype_thread":` + options.announcement + `}`
	}
	addFile(t, dir, &m, "passes.jsonl", "passes", "records", []byte(first+"\n"+second+"\n"), 2)
	if !unavailable {
		addFile(t, dir, &m, "proof.bin", "files", "blob", []byte("synthetic pass receipt"), 0)
	}
	for _, ids := range [][2]string{{"p101", "u101"}, {"p202", "u202"}} {
		rid, _ := json.Marshal(ids[0])
		uid, _ := json.Marshal(ids[1])
		proof := migrate.Proof{
			Source:         "passes",
			RecordID:       rid,
			OwnerID:        uid,
			Field:          "proof_file",
			TelegramFileID: "file.jpg",
			Unavailable:    unavailable,
		}
		if !unavailable {
			proof.Blob = "proof.bin"
		}
		m.Proofs = append(m.Proofs, proof)
	}
	writeManifest(t, dir, m)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(dir, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	plan := filepath.Join(t.TempDir(), "passes.json")
	summary, err := migrate.PlanPasses(stage, plan, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Zero(t, summary.Blocked)
	resolution := migrate.PassResolutions{
		HistoricalAnnouncements: "suppress_historical",
		Version:                 1,
		PlanSHA256:              summary.ArtifactSHA256,
		DatesVerified:           true,
		BotNamespaceVerified:    true,
		WritersStopped:          true,
	}
	raw, err := json.Marshal(resolution)
	require.NoError(t, err)
	rp := filepath.Join(t.TempDir(), "resolution.json")
	require.NoError(t, os.WriteFile(rp, raw, 0o600))
	return stage, plan, rp
}
func passImportRecordOptions(first, second string, options passImportOptions) (string, string) {
	if options.mixedGeneration {
		second = strings.ReplaceAll(
			second,
			`"date_assignment":"2026-09-02T00:00:00Z"`,
			`"date_assignment":"2026-09-05T00:00:00Z"`,
		)
		second = strings.ReplaceAll(second, `"state":"paid"`, `"state":"assigned"`)
	}
	if options.distinctAssignments {
		second = strings.ReplaceAll(
			second,
			`"date_assignment":"2026-09-02T00:00:00Z"`,
			`"date_assignment":"2026-09-02T12:00:00Z"`,
		)
	}
	if options.staleReview {
		first = strings.TrimSuffix(first, "}") + `,"proof_accepted":"2026-09-01T12:00:00Z"}`
	}
	if options.assignmentTier {
		first = strings.TrimSuffix(first, "}") + `,"assignment_tier_number":7}`
		second = strings.TrimSuffix(second, "}") + `,"assignment_tier_number":1}`
	}
	if options.foodMarkers {
		first = strings.TrimSuffix(first, "}") + `,"notified_food_last":false}`
	}
	if options.divergentActors {
		first = strings.TrimSuffix(first, "}") + `,"proof_admin_received":101}`
		second = strings.TrimSuffix(second, "}") + `,"proof_admin_received":202}`
	}
	if options.reviewed {
		first = strings.TrimSuffix(first, "}") + `,"proof_accepted":"2026-09-04T00:00:00Z","proof_admin_accepted":101}`
		second = strings.TrimSuffix(
			second,
			"}",
		) + `,"proof_accepted":"2026-09-04T00:00:00Z","proof_admin_accepted":202}`
	}
	if options.secondOnly {
		first = strings.ReplaceAll(first, "notified_deadline_close", "notified_deadline_close2")
		second = strings.ReplaceAll(second, "notified_deadline_close", "notified_deadline_close2")
	}
	return first, second
}
func TestPassImportPreservesPairUnknownActorsAndFreeMetadata(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := passImportInputs(t, false, false)
	applyOrderDependencies(t, dsn, stage)
	summary, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reconciled)
	var registrations, payments, participants, proofs, deferred int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.pass_bookings),(SELECT count(*) FROM core.pass_payment_attempts),(SELECT count(*) FROM core.pass_payment_participants),(SELECT count(*) FROM core.order_proofs),(SELECT count(*) FROM core.legacy_user_deferred_domains WHERE NOT completed)`).
			Scan(&registrations, &payments, &participants, &proofs, &deferred),
	)
	assert.Equal(t, 3, registrations)
	assert.Equal(t, 1, payments)
	assert.Equal(t, 2, participants)
	assert.Equal(t, 1, proofs)
	assert.Zero(t, deferred)
	var submitter, reviewer *string
	var decision string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT submitter,reviewed_by,decision FROM core.pass_payment_attempts`).
			Scan(&submitter, &reviewer, &decision),
	)
	assert.Nil(t, submitter)
	assert.Nil(t, reviewer)
	assert.Equal(t, "pending", decision)
	var freeFile string
	var freeAttempt *string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT m.proof_reference,b.payment_attempt FROM core.pass_bookings b JOIN core.legacy_pass_payment_metadata m ON m.owner=b.owner AND m.event_id=b.event_id WHERE b.owner='owner-303'`).
			Scan(&freeFile, &freeAttempt),
	)
	assert.Equal(t, "free_pass", freeFile)
	assert.Nil(t, freeAttempt)
	replay, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, replay.Reused)
	_, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment='changed' WHERE owner='owner-101'`)
	require.NoError(t, err)
	_, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}
func TestPassImportUnavailableReceiptAndRollback(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := passImportInputs(t, true, false)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(
		t.Context(),
		`CREATE FUNCTION core.fail_pass_import() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic'; END $$; CREATE TRIGGER fail_pass_import BEFORE INSERT ON core.pass_payment_attempts FOR EACH ROW EXECUTE FUNCTION core.fail_pass_import()`,
	)
	require.NoError(t, err)
	_, err = migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings`).Scan(&count))
	assert.Zero(t, count)
	_, err = db.Exec(t.Context(), `DROP TRIGGER fail_pass_import ON core.pass_payment_attempts`)
	require.NoError(t, err)
	_, err = migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var unavailable bool
	var proof *string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT proof_unavailable,proof_id FROM core.pass_payment_attempts`).
			Scan(&unavailable, &proof),
	)
	assert.True(t, unavailable)
	assert.Nil(t, proof)
}

func TestPassImportHistoricalReceiptIsPreservedAndReconciled(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := passImportInputs(t, false, true)
	applyOrderDependencies(t, dsn, stage)
	var pending int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.legacy_user_deferred_domains WHERE NOT completed`).
			Scan(&pending),
	)
	assert.Positive(t, pending)
	_, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var attempts, files int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.pass_payment_attempts),(SELECT count(*) FROM core.order_proofs)`).
			Scan(&attempts, &files),
	)
	assert.Zero(t, attempts)
	assert.Equal(t, 1, files)
	var falseCurrent int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.legacy_pass_payment_metadata WHERE received_at < assigned_at)+(SELECT count(*) FROM core.pass_receiver_backfills WHERE owner IN ('owner-101','owner-202'))`).
			Scan(&falseCurrent),
	)
	assert.Zero(t, falseCurrent, "historical source evidence must not be bound to the new assignment")
	_, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.order_proofs SET body='changed'::bytea`)
	require.NoError(t, err)
	_, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}

func TestPassImportParticipantActorsAndIndependentDeadlineMarkers(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := passImportFixture(
		t,
		passImportOptions{divergentActors: true, reviewed: true, secondOnly: true},
	)
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var receiver, reviewer *string
	var attempts, participants int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT receiving_admin,reviewed_by,(SELECT count(*) FROM core.pass_payment_attempts),(SELECT count(*) FROM core.pass_payment_participants) FROM core.pass_payment_attempts`).
			Scan(&receiver, &reviewer, &attempts, &participants),
	)
	assert.Nil(t, receiver)
	assert.Nil(t, reviewer)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 2, participants)
	for _, owner := range []string{"owner-101", "owner-202"} {
		var receiving, reviewing string
		var independent bool
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT m.receiving_admin,m.reviewed_by,d.first_at IS NULL AND d.second_at IS NOT NULL FROM core.legacy_pass_payment_metadata m JOIN core.pass_deadline_markers d USING(event_id,owner,assigned_at) WHERE m.owner=$1`, owner).
				Scan(&receiving, &reviewing, &independent),
		)
		assert.Equal(t, owner, receiving)
		assert.Equal(t, owner, reviewing)
		assert.True(t, independent)
	}
	_, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
}

func TestPassImportEffectiveReceiptTierAndIndependentFoodDeferral(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := passImportFixture(
		t,
		passImportOptions{staleReview: true, foodMarkers: true, assignmentTier: true, distinctAssignments: true},
	)
	applyOrderDependencies(t, dsn, stage)
	result, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.Equal(t, 2, result.PendingDomains)
	var attempts, proofs, members, tier int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.pass_payment_attempts),(SELECT count(*) FROM core.order_proofs),(SELECT count(*) FROM core.pass_payment_participants),(SELECT assignment_tier_number FROM core.legacy_pass_assignment_metadata WHERE owner='owner-101')`).
			Scan(&attempts, &proofs, &members, &tier),
	)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 1, proofs)
	assert.Equal(t, 2, members)
	assert.Equal(t, 7, tier)
	var sourceAcceptance string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_record->>'proof_accepted' FROM core.legacy_pass_import_references WHERE source_kind='booking' AND owner='owner-101'`).
			Scan(&sourceAcceptance),
	)
	assert.Equal(t, "2026-09-01T12:00:00Z", sourceAcceptance)
	result, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.Equal(t, 2, result.PendingDomains)
	// A separate owning stage may complete food without invalidating the pass-owned receipt.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.legacy_user_deferred_domains SET completed=true WHERE domain='food'; UPDATE core.legacy_pass_deferred_domains SET completed=true WHERE domain='food';`,
	)
	require.NoError(t, err)
	result, err = migrate.ReconcilePasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.Zero(t, result.PendingDomains)
}

func TestPassSharedReceiptKeepsHistoricalParticipantDetached(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := passImportFixture(t, passImportOptions{mixedGeneration: true})
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyPasses(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var attempts, members int
	var current, historical *string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.pass_payment_attempts),(SELECT count(*) FROM core.pass_payment_participants),(SELECT payment_attempt FROM core.pass_bookings WHERE owner='owner-101'),(SELECT payment_attempt FROM core.pass_bookings WHERE owner='owner-202')`).
			Scan(&attempts, &members, &current, &historical),
	)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 1, members)
	assert.NotNil(t, current)
	assert.Nil(t, historical)
}
