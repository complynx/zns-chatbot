package migrate_test

import (
	"testing"
	"time"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type legacyProofCase struct {
	name, at                                string
	attemptDate, received, validated, token bool
}

func (tc legacyProofCase) edit(t *testing.T, dir string, m *migrate.Manifest) {
	t.Helper()
	if !tc.token {
		changeOrderSource(t, dir, m, "orders.jsonl", `"payment_attempt_token":"attempt-one",`, "")
		changeOrderSource(t, dir, m, "slots.jsonl", `"attempt-one"`, `"legacy-proof:synthetic-file"`)
	}
	if !tc.attemptDate {
		changeOrderSource(t, dir, m, "orders.jsonl", `"payment_attempt_created_at":"2026-09-02T00:00:00Z",`, "")
	} else if tc.at != "2026-09-02T00:00:00Z" {
		changeOrderSource(t, dir, m, "orders.jsonl", `"payment_attempt_created_at":"2026-09-02T00:00:00Z"`,
			`"payment_attempt_created_at":"`+tc.at+`"`)
	}
	if !tc.received {
		changeOrderSource(t, dir, m, "orders.jsonl", `"proof_received":"2026-09-02T00:00:00Z",`, "")
	}
	if !tc.validated {
		changeOrderSource(t, dir, m, "orders.jsonl", `,"validated_at":"2026-09-03T00:00:00Z"`, "")
	}
	if tc.at != "2026-09-02T00:00:00Z" {
		changeOrderSource(t, dir, m, "slots.jsonl", `"reservation_attempt_created_at":"2026-09-02T00:00:00Z"`,
			`"reservation_attempt_created_at":"`+tc.at+`"`)
	}
}

func TestApplyOrdersPreservesLegacyProofIdentityAndTimeFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []legacyProofCase{
		{"attempt date", "2026-09-04T00:00:00Z", true, true, true, false},
		{"proof date", "2026-09-02T00:00:00Z", false, true, true, false},
		{"validation date", "2026-09-03T00:00:00Z", false, false, true, false},
		{"creation date", "2026-09-01T00:00:00Z", false, false, false, false},
		{"current token missing attempt date", "2026-09-02T00:00:00Z", false, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			token := "legacy-proof:synthetic-file"
			if tc.token {
				token = "attempt-one"
			}
			stage, plan, resolutions := orderImportInputs(t, false, tc.edit)
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
			var actualToken, slotToken string
			var attemptAt, slotAt, proofAt time.Time
			require.NoError(t, db.QueryRow(t.Context(), `SELECT o.attempt,o.attempt_at,s.reservation_attempt_token,
			s.reservation_attempt_created_at,p.created_at FROM core.orders o
			JOIN core.order_capacity_slots s ON s.reservation_id=o.id
			JOIN core.order_proofs p ON p.id=o.proof_file WHERE o.state='paid'`).
				Scan(&actualToken, &attemptAt, &slotToken, &slotAt, &proofAt))
			at, err := time.Parse(time.RFC3339, tc.at)
			require.NoError(t, err)
			assert.Equal(t, token, actualToken)
			assert.Equal(t, token, slotToken)
			assert.True(t, at.Equal(attemptAt))
			assert.True(t, at.Equal(slotAt))
			if tc.received {
				assert.Equal(t, "2026-09-02T00:00:00Z", proofAt.UTC().Format(time.RFC3339))
			} else {
				assert.True(t, at.Equal(proofAt))
			}
			var hasToken, hasAttemptDate, hasProofDate bool
			require.NoError(t, db.QueryRow(t.Context(), `SELECT source_record ? 'payment_attempt_token',
			source_record ? 'payment_attempt_created_at',source_record ? 'proof_received'
			FROM core.legacy_order_import_references WHERE source_domain='orders'
			AND target_id='legacy-order:77:000000000000000000000001'`).Scan(&hasToken, &hasAttemptDate, &hasProofDate))
			assert.Equal(t, tc.token, hasToken)
			assert.Equal(t, tc.attemptDate, hasAttemptDate)
			assert.Equal(t, tc.received, hasProofDate)
			_, err = migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
			_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
		})
	}
}
