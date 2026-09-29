package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCodeQAEnforcedSettlementOneConnection(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db, Enforce: true}
	installCreditPrice(t, service)
	a := creditAttempt("alice")
	require.NoError(t, service.Reserve(t.Context(), a))
	require.NoError(t, service.Dispatch(t.Context(), a.ID))
	cfg := db.Config()
	cfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	defer single.Close()
	service.DB = single
	input, cached, output := int64(10), int64(0), int64(0)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = service.Settle(
		ctx,
		a.ID,
		credits.Settlement{
			CostBasis: "unknown",
			Usage: credits.Usage{
				Basis:       "reported",
				Model:       a.Model,
				ServiceTier: "default",
				Input:       &input,
				Cached:      &cached,
				Output:      &output,
			},
		},
	)
	t.Logf("settlement error: %v", err)
	var state string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT state FROM credits.attempts WHERE id=$1`, a.ID).Scan(&state))
	t.Logf("durable attempt state: %s", state)
	service.DB = db
	require.NoError(
		t,
		service.Settle(
			t.Context(),
			a.ID,
			credits.Settlement{
				CostBasis: "unknown",
				Usage: credits.Usage{
					Basis:       "reported",
					Model:       a.Model,
					ServiceTier: "default",
					Input:       &input,
					Cached:      &cached,
					Output:      &output,
				},
			},
		),
		"control: same receipt settles when a spare pool connection is available",
	)
	t.Log("control: same receipt settled with the normal pool")
	require.NoError(t, err, "valid numeric receipt must settle without requiring a second pool connection")
}
