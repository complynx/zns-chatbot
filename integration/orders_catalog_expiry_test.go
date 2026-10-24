package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestOrdersCatalogExpiresWhileAuthorityCheckWaits(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		name := "create"
		if existing {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			f := setupPayment(t, newPeers(t))
			admin := ownerProfileScope(t, f.H, f.AdminID)
			change := db.OrdersCatalogChange{
				EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, Document: catalogDocument(),
			}
			var before db.OrdersCatalog
			if existing {
				op := uuid.New()
				status, result := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
				require.Equal(t, "succeeded", status)
				before = result
				change.ExpectedVersion = result.Version
				change.Document.Items[0].BasePriceMinor++
			}
			op := uuid.New()
			prepared := prepareCatalog(t, f, op, change)
			var cutoff time.Time
			require.NoError(t, f.H.store.Pool().QueryRow(ctx,
				"UPDATE shuttle_orders_catalog_preparations SET expires_at=clock_timestamp()+interval '2 seconds' WHERE "+
					"id=$1 RETURNING expires_at",
				prepared.ID).
				Scan(&cutoff))
			checked := false
			_, err := f.H.store.ChangeOrdersCatalog(ctx, admin, op, prepared.ID, change, func(context.Context) error {
				checked = true
				require.Eventually(t, func() bool {
					var now time.Time
					err := f.H.store.Pool().QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
					return err == nil && !now.Before(cutoff)
				}, 4*time.Second, 20*time.Millisecond)
				return nil
			})
			require.True(t, checked, "The proposal must still be valid when authority verification begins")
			require.ErrorIs(t, err, db.ErrConflict)
			current, err := f.H.store.OrdersCatalogConfiguration(ctx, admin, change.EventID)
			if existing {
				require.NoError(t, err)
				require.Equal(t, before, current)
			} else {
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
			_, err = f.H.store.OrdersCatalogOperation(ctx, admin, op)
			require.ErrorIs(t, err, pgx.ErrNoRows, "Expired consent must not leave a successful receipt")
		})
	}
}
