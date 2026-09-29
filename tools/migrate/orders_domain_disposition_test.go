package migrate_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func appendFoodToOrders(t *testing.T, directory string, manifest *migrate.Manifest) {
	t.Helper()
	stage, _, _ := foodImportInputs(t)
	root := filepath.Join(stage, "snapshot")
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	require.NoError(t, err)
	var food migrate.Manifest
	require.NoError(t, json.Unmarshal(raw, &food))
	for _, file := range food.Files {
		if file.Source == "users" || file.Source == "events" {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(root, file.Path))
		require.NoError(t, readErr)
		name := file.Path
		if file.Kind == "records" {
			name = "food-" + name
			body = []byte(strings.ReplaceAll(string(body), "000000000000000000000001", "000000000000000000000011"))
		}
		addFile(t, directory, manifest, name, file.Source, file.Kind, body, file.Records)
	}
	for _, proof := range food.Proofs {
		proof.RecordID = json.RawMessage(`{"$oid":"000000000000000000000011"}`)
		manifest.Proofs = append(manifest.Proofs, proof)
	}
}

func TestOrdersDelegatesValidatedFoodEvidence(t *testing.T) {
	t.Parallel()
	stage, path, resolution := orderImportInputs(t, false, appendFoodToOrders)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var plan migrate.OrderPlan
	require.NoError(t, json.Unmarshal(raw, &plan))
	delegated := 0
	for _, row := range plan.Records {
		require.Empty(t, row.Blockers)
		if row.DelegatedDomain != "" {
			delegated++
			require.EqualValues(t, "food", row.DelegatedDomain)
			require.NotEmpty(t, row.Legacy.RecordSHA256)
			require.Nil(t, row.Order)
			require.Nil(t, row.Catalog)
		}
	}
	require.Equal(t, 2, delegated)
	require.Len(t, plan.Dispositions, 3)
	// Malformed DSN proves all preparation succeeds without database access.
	_, err = migrate.ApplyOrders(t.Context(), "offline-invalid-dsn", stage, path, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_database_unavailable")
}

func TestOrdersDelegationRejectsUnownedEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(*testing.T, string, *migrate.Manifest)
		want string
	}{
		{"resource", func(t *testing.T, dir string, m *migrate.Manifest) {
			t.Helper()
			addFile(t, dir, m, "unknown.json", "configuration", "resource", []byte(`{}`), 0)
		}, "order_configuration_records_required"},
		{"proof", func(t *testing.T, _ string, m *migrate.Manifest) {
			t.Helper()
			p := m.Proofs[len(m.Proofs)-1]
			p.Field = "unknown_proof"
			m.Proofs = append(m.Proofs, p)
		}, "order_proof_inventory_unresolved"},
		{"duplicate-proof", func(t *testing.T, _ string, m *migrate.Manifest) {
			t.Helper()
			m.Proofs = append(m.Proofs, m.Proofs[len(m.Proofs)-1])
		}, "food_proof_ambiguous"},
		{"missing-proof", func(t *testing.T, _ string, m *migrate.Manifest) { t.Helper(); m.Proofs = m.Proofs[:len(m.Proofs)-1] }, "food_proof_disposition_required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stage, path, resolution := orderImportInputs(t, false, appendFoodToOrders, test.edit)
			_, err := migrate.ApplyOrders(
				t.Context(),
				"offline-invalid-dsn",
				stage,
				path,
				resolution,
				migrate.DefaultLimits(),
			)
			require.EqualError(t, err, test.want)
		})
	}
}

func TestOrdersDelegationBlocksAmbiguousRecords(t *testing.T) {
	t.Parallel()
	for _, input := range []struct{ source, raw string }{
		{"orders", `{"_id":{"$oid":"000000000000000000000012"},"user_id":101,"pass_key":"event_one","event_key":"event_one","choice":{},"created_at":"2026-09-01T00:00:00Z"}`},
		{"configuration", `{"_id":"unknown","kind":"future_domain"}`},
	} {
		t.Run(input.source, func(t *testing.T) {
			t.Parallel()
			stage, path, resolution := orderImportInputs(
				t,
				false,
				appendFoodToOrders,
				func(t *testing.T, dir string, manifest *migrate.Manifest) {
					t.Helper()
					addFile(t, dir, manifest, "ambiguous.jsonl", input.source, "records", []byte(input.raw+"\n"), 1)
				},
			)
			_, err := migrate.ApplyOrders(
				t.Context(),
				"offline-invalid-dsn",
				stage,
				path,
				resolution,
				migrate.DefaultLimits(),
			)
			require.EqualError(t, err, "apply_record_blocked")
		})
	}
}

func TestOrdersDelegatesMassageConfiguration(t *testing.T) {
	t.Parallel()
	stage, path, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, manifest *migrate.Manifest) {
		t.Helper()
		raw := `{"_id":"massage-config","kind":"legacy_massage","event_key":"event_one","daily_limit":3,"prior_long_seconds":3600,"prior_short_seconds":600,"parties":[{"start":"2035-12-01T18:00:00+03:00","end":"2035-12-02T02:00:00+03:00","massage_tables":1,"is_open":false}]}`
		addFile(t, dir, manifest, "massage-config.jsonl", "configuration", "records", []byte(raw+"\n"), 1)
	})
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"delegated_domain":"massage"`)
	_, err = migrate.ApplyOrders(t.Context(), "offline-invalid-dsn", stage, path, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_database_unavailable")
}

func TestOrdersDelegationRealPostgres(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, path, resolution := orderImportInputs(t, false, appendFoodToOrders)
	applyOrderDependencies(t, dsn, stage)
	result, err := migrate.ApplyOrders(t.Context(), dsn, stage, path, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Applied)
	var modern, legacy int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.orders),(SELECT count(*) FROM core.food_orders)`).
			Scan(&modern, &legacy),
	)
	require.Equal(t, 3, modern)
	require.Zero(t, legacy)
	result, err = migrate.ReconcileOrders(t.Context(), dsn, stage, path, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	require.True(t, result.Reconciled)
	// Modern reconciliation deliberately makes no claim about food completion.
}

func TestModernOrdersPlanBytesUnchanged(t *testing.T) {
	t.Parallel()
	_, path, _ := orderImportInputs(t, false)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "delegated_domain")
	require.NotContains(t, string(raw), "domain_dispositions")
	// This digest freezes the complete historical modern-only fixture encoding.
	require.Equal(
		t,
		"71ab6f5bd272a347bb4746bcaf227e9d9b049db2d4d428508b78e6ece84c12db",
		fmt.Sprintf("%x", sha256.Sum256(raw)),
	)
}
