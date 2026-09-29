package integration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// Seed the exact target shape of a tokenless source proof: the effective source
// reservation token is distinct from its new owner-bound Core proof identity.
func TestLegacyImportedProofCommandsPreserveAttemptAndReservation(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"accept", "reject", "cancel_proof"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			service := orders.Service{DB: db}
			order, err := service.Execute(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "create",
				Choice: orderChoice("shuttle"),
			})
			require.NoError(t, err)
			proof := orderCommand("proof", order)
			proof.ProofFile = uploadProof(t, service, "alice")
			order, err = service.Execute(t.Context(), "alice", proof)
			require.NoError(t, err)
			legacyToken := "legacy-proof:" + strings.Repeat("source-file-", 10)
			legacyAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
			_, err = db.Exec(t.Context(), `UPDATE core.orders SET attempt=$2,attempt_at=$3 WHERE id=$1`,
				order.ID, legacyToken, legacyAt)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `UPDATE core.order_capacity_slots SET reservation_attempt_token=$2,
			reservation_attempt_created_at=$3,reserved_at=$3 WHERE reservation_id=$1`, order.ID, legacyToken, legacyAt)
			require.NoError(t, err)
			listed, err := service.List(t.Context(), "alice", "sandbox-festival")
			require.NoError(t, err)
			require.Len(t, listed, 1)
			order = listed[0]
			assert.Equal(t, legacyToken, order.Attempt)
			assert.NotEqual(t, order.ProofFile, order.Attempt)
			command := orderCommand(action, order)
			actor := "bob"
			if action == "cancel_proof" {
				actor = "alice"
			}
			wrongAttempt := command
			wrongAttempt.Key = "wrong-attempt"
			wrongAttempt.Attempt = ""
			_, err = service.Execute(t.Context(), actor, wrongAttempt)
			requireCode(t, err, "stale_attempt")
			finished, err := service.Execute(t.Context(), actor, command)
			require.NoError(t, err)
			replayed, err := service.Execute(t.Context(), actor, command)
			require.NoError(t, err)
			assert.Equal(t, finished.Version, replayed.Version)
			var claims int
			require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_capacity_slots
			WHERE reservation_id=$1`, order.ID).Scan(&claims))
			if action == "accept" {
				assert.Equal(t, "paid", finished.State)
				assert.Equal(t, 1, claims)
				var token string
				var reserved time.Time
				require.NoError(t, db.QueryRow(t.Context(), `SELECT reservation_attempt_token,reserved_at
				FROM core.order_capacity_slots WHERE reservation_id=$1`, order.ID).Scan(&token, &reserved))
				assert.Equal(t, legacyToken, token)
				assert.True(t, legacyAt.Equal(reserved))
				return
			}
			assert.Equal(t, "unpaid", finished.State)
			assert.Zero(t, claims)
			newProof := orderCommand("proof", finished)
			newProof.ProofFile = proof.ProofFile
			current, err := service.Execute(t.Context(), "alice", newProof)
			require.NoError(t, err)
			stale := orderCommand(action, current)
			stale.Attempt = legacyToken
			_, err = service.Execute(t.Context(), actor, stale)
			requireCode(t, err, "stale_attempt")
		})
	}
}
