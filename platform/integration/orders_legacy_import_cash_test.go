package integration_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func legacyCashFixture(t *testing.T, preservePriority bool) (orders.Service, orders.Order) {
	t.Helper()
	db := database(t)
	service := orders.Service{DB: db}
	order, err := service.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "create", Choice: orderChoice("shuttle"),
	})
	require.NoError(t, err)
	cash := orderCommand("cash", order)
	cash.PaymentAdmin = "bob"
	order, err = service.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.orders SET attempt='',attempt_at='2026-09-01Z' WHERE id=$1`, order.ID)
	require.NoError(t, err)
	source := map[string]any{"_id": "synthetic-import", "user_id": 101, "proof_file": "cash",
		"proof_admin": 202, "proof_country": "be", "created_at": "2026-09-01T00:00:00Z"}
	if preservePriority {
		source["payment_attempt_created_at"] = "2026-09-01T00:00:00Z"
	}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.legacy_order_import_references
	(source_key,bot_id,event_id,source_domain,source_record_sha256,target_id,source_record)
	VALUES($1,77,'sandbox-festival','orders',$2,$3,$4)`, strings.Repeat("a", 64), strings.Repeat("b", 64), order.ID, raw)
	require.NoError(t, err)
	listed, err := service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	return service, listed[0]
}

func TestLegacyImportedCashAcceptanceAndReplay(t *testing.T) {
	t.Parallel()
	for _, preserve := range []bool{true, false} {
		t.Run(map[bool]string{true: "source priority", false: "new validation time"}[preserve], func(t *testing.T) {
			t.Parallel()
			service, order := legacyCashFixture(t, preserve)
			command := orderCommand("accept", order)
			_, err := service.Execute(t.Context(), "alice", command)
			requireCode(t, err, "forbidden")
			var results [2]orders.Order
			var failures [2]error
			var workers sync.WaitGroup
			for i := range results {
				workers.Go(func() { results[i], failures[i] = service.Execute(t.Context(), "bob", command) })
			}
			workers.Wait()
			for _, failure := range failures {
				require.NoError(t, failure)
			}
			assert.Equal(t, results[0].Attempt, results[1].Attempt)
			assert.Equal(t, order.Version+1, results[0].Version)
			assert.Equal(t, "paid", results[0].State)
			assert.NotEmpty(t, results[0].Attempt)
			if preserve {
				assert.True(t, order.AttemptAt.Equal(*results[0].AttemptAt))
			} else {
				assert.True(t, results[0].AttemptAt.After(*order.AttemptAt))
			}
			var token string
			var at time.Time
			require.NoError(
				t,
				service.DB.QueryRow(t.Context(), `SELECT reservation_attempt_token,reservation_attempt_created_at
			FROM core.order_capacity_slots WHERE reservation_id=$1`, order.ID).Scan(&token, &at),
			)
			assert.Equal(t, results[0].Attempt, token)
			assert.True(t, at.Equal(*results[0].AttemptAt))
			var sourceToken bool
			require.NoError(t, service.DB.QueryRow(t.Context(), `SELECT source_record ? 'payment_attempt_token'
			FROM core.legacy_order_import_references WHERE target_id=$1`, order.ID).Scan(&sourceToken))
			assert.False(t, sourceToken)
		})
	}
}

func TestLegacyImportedCashRejectRetryAndProvenanceGuard(t *testing.T) {
	t.Parallel()
	service, order := legacyCashFixture(t, false)
	command := orderCommand("reject", order)
	finished, err := service.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.Equal(t, "unpaid", finished.State)
	replayed, err := service.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.Equal(t, finished.Version, replayed.Version)
	cash := orderCommand("cash", finished)
	cash.PaymentAdmin = "bob"
	current, err := service.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	assert.NotEmpty(t, current.Attempt)
	stale := orderCommand("accept", current)
	stale.Attempt = ""
	_, err = service.Execute(t.Context(), "bob", stale)
	requireCode(t, err, "stale_attempt")
	_, err = service.DB.Exec(t.Context(), `UPDATE core.orders SET attempt='' WHERE id=$1`, current.ID)
	require.NoError(t, err)
	_, err = service.DB.Exec(
		t.Context(),
		`DELETE FROM core.legacy_order_import_references WHERE target_id=$1`,
		current.ID,
	)
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "bob", stale)
	requireCode(t, err, "stale_attempt")
}

func TestLegacyImportedCashSurvivesCapacityVersionChange(t *testing.T) {
	t.Parallel()
	service, imported := legacyCashFixture(t, false)
	_, err := service.DB.Exec(
		t.Context(),
		`UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1')`,
	)
	require.NoError(t, err)
	other, err := service.Execute(t.Context(), "bob", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "other", Choice: orderChoice("shuttle"),
	})
	require.NoError(t, err)
	proof := orderCommand("proof", other)
	proof.ProofFile = uploadProof(t, service, "bob")
	_, err = service.Execute(t.Context(), "bob", proof)
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "bob", orderCommand("accept", imported))
	requireCode(t, err, "stale_version")
	listed, err := service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Greater(t, listed[0].Version, imported.Version)
	assert.Empty(t, listed[0].Attempt)
	assert.Equal(t, "cash", listed[0].State)
	assert.NotContains(t, listed[0].Choice.Extras, "shuttle")
	_, err = service.Execute(t.Context(), "bob", orderCommand("accept", listed[0]))
	require.NoError(t, err)
}
