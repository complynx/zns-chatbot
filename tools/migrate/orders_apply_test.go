package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderImportInputs(
	t *testing.T,
	second bool,
	edits ...func(*testing.T, string, *migrate.Manifest),
) (string, string, string) {
	t.Helper()
	dir, m := snapshot(t)
	m.Files = nil
	addFile(
		t,
		dir,
		&m,
		"users.jsonl",
		"users",
		"records",
		[]byte(`{"_id":"user-a","bot_id":77,"user_id":101,"print_name":"Synthetic"}`+"\n"),
		1,
	)
	events := syntheticEvent + "\n"
	if second {
		events += strings.ReplaceAll(
			strings.ReplaceAll(syntheticEvent, "event-one", "event-two"),
			"event_one",
			"event_two",
		) + "\n"
	}
	count := int64(1)
	if second {
		count = 2
	}
	addFile(t, dir, &m, "events.jsonl", "events", "records", []byte(events), count)
	menu, err := os.ReadFile("../../platform/internal/orders/menu_belarus.json")
	require.NoError(t, err)
	catalog := `{"_id":"catalog-one","kind":"orders_catalog_v1","byn_to_rub":30,"event_key":"event_one","deadline":"2026-12-01T00:00:00Z","menu":` + string(
		menu,
	) + `,"extras":{"shuttle":{"price":65,"capacity":2},"preparty":{"price":35}},"transfer_instructions":"Synthetic only","transfer_instructions_localized":{"en":"Synthetic only","ru":"Только тест"},"payment_admins":[{"user_id":101,"country":"be","region":"Synthetic"}]}`
	var compactCatalog strings.Builder
	var parsed any
	require.NoError(t, json.Unmarshal([]byte(catalog), &parsed))
	raw, err := json.Marshal(parsed)
	require.NoError(t, err)
	compactCatalog.Write(raw)
	compactCatalog.WriteString("\n")
	if second {
		compactCatalog.WriteString(
			strings.ReplaceAll(strings.ReplaceAll(string(raw), "catalog-one", "catalog-two"), "event_one", "event_two"),
		)
		compactCatalog.WriteString("\n")
	}
	addFile(t, dir, &m, "configuration.jsonl", "configuration", "records", []byte(compactCatalog.String()), count)
	orders := `{"_id":{"$oid":"000000000000000000000001"},"event_key":"event_one","user_id":101,"created_at":"2026-09-01T00:00:00Z","choice":{"customer":"Synthetic","days":{"old-day":{"mealtimes":{"lunch":{"dishes":[{"name":"historical-removed","count":1,"price":9.99,"total":9.99}],"service":{"items":[],"total":0},"total":9.99}},"total":9.99}},"extras":{"shuttle":65,"total":65},"total":74.99},"proof_file":"synthetic-file","proof_chat_id":101,"proof_message_id":42,"proof_received":"2026-09-02T00:00:00Z","payment_attempt_token":"attempt-one","payment_attempt_created_at":"2026-09-02T00:00:00Z","validation":true,"validated_at":"2026-09-03T00:00:00Z"}` + "\n" +
		`{"_id":{"$oid":"000000000000000000000002"},"event_key":"event_one","user_id":101,"created_at":"2026-09-01T00:00:00Z","choice":{"days":{},"extras":{"preparty":35,"total":35},"total":35}}` + "\n" +
		`{"_id":{"$oid":"000000000000000000000003"},"event_key":"event_one","user_id":101,"created_at":"2026-09-01T00:00:00Z","choice":{"days":{},"extras":{"preparty":35,"total":35},"total":35},"proof_file":"cash","proof_country":"be","proof_admin":101,"cash_requested_at":"2026-09-02T00:00:00Z","payment_attempt_token":"cash-one","payment_attempt_created_at":"2026-09-02T00:00:00Z"}` + "\n"
	addFile(t, dir, &m, "orders.jsonl", "orders", "records", []byte(orders), 3)
	slots := `{"_id":"event_one:shuttle:0","event_key":"event_one","service":"shuttle","seat":0,"reservation_id":{"$oid":"000000000000000000000999"},"reserved_at":"2026-09-01T00:00:00Z"}` + "\n" +
		`{"_id":"event_one:shuttle:1","event_key":"event_one","service":"shuttle","seat":1,"reservation_id":{"$oid":"000000000000000000000001"},"reservation_attempt_token":"attempt-one","reservation_attempt_created_at":"2026-09-02T00:00:00Z","reserved_at":"2026-09-02T00:00:01Z"}` + "\n"
	if second {
		slots += `{"_id":"event_two:shuttle:0","event_key":"event_two","service":"shuttle","seat":0}` + "\n" + `{"_id":"event_two:shuttle:1","event_key":"event_two","service":"shuttle","seat":1}` + "\n"
	}
	addFile(t, dir, &m, "slots.jsonl", "order_capacity", "records", []byte(slots), count*2)
	addFile(t, dir, &m, "proof.bin", "files", "blob", []byte("synthetic-receipt-bytes"), 0)
	m.Proofs = []migrate.Proof{
		{
			Source:         "orders",
			RecordID:       json.RawMessage(`{"$oid":"000000000000000000000001"}`),
			OwnerID:        json.RawMessage(`"user-a"`),
			Field:          "proof_file",
			TelegramFileID: "synthetic-file",
			ChatID:         101,
			MessageID:      42,
			Blob:           "proof.bin",
		},
	}
	for _, edit := range edits {
		edit(t, dir, &m)
	}
	writeManifest(t, dir, m)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err = migrate.Stage(dir, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	plan := filepath.Join(t.TempDir(), "orders.json")
	summary, err := migrate.PlanOrders(stage, plan, migrate.DefaultLimits())
	require.NoError(t, err)
	if len(edits) == 0 {
		require.Zero(t, summary.Blocked)
	}
	resolution := migrate.OrderResolutions{
		Version:               1,
		PlanSHA256:            summary.ArtifactSHA256,
		DatesVerified:         true,
		ConfigurationVerified: true,
		AdminGrantsVerified:   true,
		BotNamespaceVerified:  true,
		WritersStopped:        true,
	}
	raw, err = json.Marshal(resolution)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "resolution.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return stage, plan, path
}

func applyOrderDependencies(t *testing.T, dsn, stage string) {
	t.Helper()
	userPlan := filepath.Join(t.TempDir(), "users.jsonl")
	us, err := migrate.PlanUsers(stage, userPlan, migrate.DefaultLimits())
	require.NoError(t, err)
	r := migrate.UserResolutions{Version: 1, PlanSHA256: us.ArtifactSHA256, IdentityAttested: true}
	allowed := true
	for _, row := range userRows(t, userPlan) {
		if row.Candidate != nil {
			r.Users = append(
				r.Users,
				migrate.UserResolution{
					LegacyKey: row.Legacy.Key,
					Owner:     "owner-" + strconv.FormatInt(row.Candidate.TelegramID, 10),
					Issuer:    "https://synthetic.invalid",
					Subject:   row.Legacy.Key,
					CanBook:   &allowed,
				},
			)
		}
	}
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	rp := filepath.Join(t.TempDir(), "users-resolution.json")
	require.NoError(t, os.WriteFile(rp, raw, 0o600))
	_, err = migrate.ApplyUsers(t.Context(), dsn, stage, userPlan, rp, migrate.DefaultLimits())
	require.NoError(t, err)
	ep := filepath.Join(t.TempDir(), "events.json")
	es, err := migrate.PlanEvents(stage, ep, migrate.DefaultLimits())
	require.NoError(t, err)
	raw, err = os.ReadFile(ep)
	require.NoError(t, err)
	var plan migrate.EventPlan
	require.NoError(t, json.Unmarshal(raw, &plan))
	er := migrate.EventResolutions{
		Version:               1,
		PlanSHA256:            es.ArtifactSHA256,
		DatesVerified:         true,
		ConfigurationVerified: true,
		AdminGrantsVerified:   true,
	}
	for i, row := range plan.Events {
		position := int32(i)
		er.Events = append(er.Events, migrate.EventResolution{LegacyKey: row.Legacy.Key, DisplayOrder: &position})
	}
	raw, err = json.Marshal(er)
	require.NoError(t, err)
	rp = filepath.Join(t.TempDir(), "event-resolution.json")
	require.NoError(t, os.WriteFile(rp, raw, 0o600))
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, ep, rp, migrate.DefaultLimits())
	require.NoError(t, err)
}

func TestApplyOrdersPreservesHistoricalChoicesProofCashAndSeats(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := orderImportInputs(t, false)
	applyOrderDependencies(t, dsn, stage)
	summary, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Applied)
	assert.True(t, summary.Reconciled)
	var state, attempt, total, holder string
	var body []byte
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state,attempt,choice->>'total' FROM core.orders WHERE id='legacy-order:77:000000000000000000000001'`).
			Scan(&state, &attempt, &total),
	)
	assert.Equal(t, "paid", state)
	assert.Equal(t, "attempt-one", attempt)
	assert.Equal(t, "74.99", total)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_id FROM core.order_capacity_slots WHERE seat=1`).Scan(&holder),
	)
	assert.Equal(t, "legacy-order:77:000000000000000000000001", holder)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT body FROM core.order_proofs`).Scan(&body))
	assert.Equal(t, []byte("synthetic-receipt-bytes"), body)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM core.orders WHERE id='legacy-order:77:000000000000000000000003'`).
			Scan(&state),
	)
	assert.Equal(t, "cash", state)
	summary, err = migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Reused)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.order_capacity_slots SET reserved_at=reserved_at+interval '1 second' WHERE seat=1`,
	)
	require.NoError(t, err)
	summary, err = migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
	assert.False(t, summary.Reconciled)
}

func TestApplyOrdersConcurrentAndPartialResume(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := orderImportInputs(t, true)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.order_events(id,deadline,menu,extras) VALUES('event_two',now(),'{}','{}')`,
	)
	require.NoError(t, err)
	summary, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_target_conflict")
	assert.EqualValues(t, 1, summary.Applied)
	_, err = db.Exec(t.Context(), `DELETE FROM core.order_events WHERE id='event_two'`)
	require.NoError(t, err)
	var summaries [2]migrate.OrderApplySummary
	var failures [2]error
	var wg sync.WaitGroup
	for i := range summaries {
		wg.Go(func() {
			summaries[i], failures[i] = migrate.ApplyOrders(
				t.Context(),
				dsn,
				stage,
				plan,
				resolution,
				migrate.DefaultLimits(),
			)
		})
	}
	wg.Wait()
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	assert.EqualValues(t, 1, summaries[0].Applied+summaries[1].Applied)
	assert.EqualValues(t, 3, summaries[0].Reused+summaries[1].Reused)
}
