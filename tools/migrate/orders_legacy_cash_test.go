package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOrdersPreservesTokenlessCash(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []bool{true, false} {
		t.Run(map[bool]string{true: "cash sentinel", false: "cash date"}[sentinel], func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
				changeOrderSource(t, dir, m, "orders.jsonl", `,"payment_attempt_token":"cash-one"`, "")
				changeOrderSource(
					t,
					dir,
					m,
					"orders.jsonl",
					`,"payment_attempt_created_at":"2026-09-02T00:00:00Z"}`,
					`}`,
				)
				if !sentinel {
					changeOrderSource(t, dir, m, "orders.jsonl", `"proof_file":"cash",`, "")
				}
			})
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			var token, state, proof string
			var at time.Time
			require.NoError(t, db.QueryRow(t.Context(), `SELECT attempt,attempt_at,state,proof_file FROM core.orders
			WHERE id='legacy-order:77:000000000000000000000003'`).Scan(&token, &at, &state, &proof))
			assert.Empty(t, token)
			assert.Empty(t, proof)
			assert.Equal(t, "cash", state)
			assert.Equal(t, "2026-09-01T00:00:00Z", at.UTC().Format(time.RFC3339))
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
		})
	}
}

func validationOnlySource(t *testing.T, dir string, m *migrate.Manifest, metadata map[string]string, date string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "orders.jsonl"))
	require.NoError(t, err)
	line, _, _ := strings.Cut(string(raw), "\n")
	var record map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(line), &record))
	for _, key := range []string{"proof_file", "proof_received", "proof_chat_id", "proof_message_id",
		"payment_attempt_token", "payment_attempt_created_at", "validated_at"} {
		delete(record, key)
	}
	if date != "" {
		record["validated_at"], err = json.Marshal(date)
		require.NoError(t, err)
	}
	if metadata != nil {
		record["_migration"], err = json.Marshal(metadata)
		require.NoError(t, err)
	}
	replacement, err := json.Marshal(record)
	require.NoError(t, err)
	changeOrderSource(t, dir, m, "orders.jsonl", line, string(replacement))
	m.Proofs = nil
	m.Files = slices.DeleteFunc(m.Files, func(f migrate.File) bool { return f.Path == "proof.bin" })
	require.NoError(t, os.Remove(filepath.Join(dir, "proof.bin")))
	for i := range m.Coverage {
		if m.Coverage[i].Domain == "files" {
			m.Coverage[i].Status, m.Coverage[i].Reason = "absent", "Synthetic validation has no receipt"
		}
	}
}

func TestApplyOrdersPreservesValidationOnlyIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, token, date, offset, at string }{
		{"undated", "legacy-validation:true", "", "", "2026-09-01T00:00:00Z"},
		{"naive source clock", "legacy-validation:2026-09-03 03:00:00", "2026-09-03T00:00:00Z", "+03:00", "2026-09-03T00:00:00Z"},
		{"aware source clock", "legacy-validation:2026-09-03 03:00:00.123456+03:00", "2026-09-03T00:00:00.123456Z", "", "2026-09-03T00:00:00.123456Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
				var metadata map[string]string
				if tc.date != "" {
					metadata = map[string]string{"payment_reservation_token": tc.token}
					if tc.offset != "" {
						metadata["validated_at_source_offset"] = tc.offset
					}
				}
				validationOnlySource(t, dir, m, metadata, tc.date)
				changeOrderSource(t, dir, m, "slots.jsonl", `"attempt-one"`, `"`+tc.token+`"`)
				changeOrderSource(t, dir, m, "slots.jsonl", `"reservation_attempt_created_at":"2026-09-02T00:00:00Z"`,
					`"reservation_attempt_created_at":"`+tc.at+`"`)
			})
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			var token, state, proof string
			require.NoError(t, db.QueryRow(t.Context(), `SELECT attempt,state,proof_file FROM core.orders
			WHERE id='legacy-order:77:000000000000000000000001'`).Scan(&token, &state, &proof))
			assert.Equal(t, tc.token, token)
			assert.Equal(t, "paid", state)
			assert.Empty(t, proof)
			var count int
			require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_proofs`).Scan(&count))
			assert.Zero(t, count)
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
		})
	}
}

func TestApplyOrdersRejectsUnprovedValidationIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, token, offset string }{
		{"wrong instant", "legacy-validation:2026-09-04 00:00:00", "Z"},
		{"wrong offset", "legacy-validation:2026-09-03 03:00:00", "+02:00"},
		{"missing offset", "legacy-validation:2026-09-03 00:00:00", ""},
		{"arbitrary token", "invented", "Z"},
		{"wrong status token", "legacy-proof:file", "Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
				validationOnlySource(t, dir, m, map[string]string{
					"payment_reservation_token": tc.token, "validated_at_source_offset": tc.offset,
				}, "2026-09-03T00:00:00Z")
			})
			_, err := migrate.ApplyOrders(
				t.Context(),
				"invalid database",
				stage,
				plan,
				resolution,
				migrate.DefaultLimits(),
			)
			require.EqualError(t, err, "apply_record_blocked")
		})
	}
}

func TestApplyOrdersPreservesFalsyProofValidation(t *testing.T) {
	t.Parallel()
	for _, proof := range []string{"null", "false"} {
		t.Run(proof, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
				validationOnlySource(t, dir, m, nil, "")
				changeOrderSource(
					t,
					dir,
					m,
					"orders.jsonl",
					`"validation":true`,
					`"proof_file":`+proof+`,"validation":true`,
				)
				changeOrderSource(t, dir, m, "slots.jsonl", `"attempt-one"`, `"legacy-validation:true"`)
				changeOrderSource(t, dir, m, "slots.jsonl", `"reservation_attempt_created_at":"2026-09-02T00:00:00Z"`,
					`"reservation_attempt_created_at":"2026-09-01T00:00:00Z"`)
			})
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			var preserved string
			require.NoError(t, db.QueryRow(t.Context(), `SELECT (source_record->'proof_file')::text
			FROM core.legacy_order_import_references WHERE target_id='legacy-order:77:000000000000000000000001'
			AND source_domain='orders'`).Scan(&preserved))
			assert.Equal(t, proof, preserved)
		})
	}
}

func TestApplyOrdersRequiresValidationSlotAgreement(t *testing.T) {
	t.Parallel()
	stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
		validationOnlySource(t, dir, m, nil, "")
	})
	_, err := migrate.ApplyOrders(t.Context(), "invalid database", stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "order_reservation_attempt_unresolved")
}
