package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

func TestReplacementInventoryPreservesContextFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name  string
		cause error
	}{
		{name: "canceled", cause: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			role := "synthetic_qa_replacement_cancel_" +
				strings.TrimPrefix(db.Config().ConnConfig.Database, "synthetic_qa_zns_")
			_, err := db.Exec(t.Context(), "CREATE ROLE "+pgx.Identifier{role}.Sanitize())
			require.NoError(t, err)
			t.Cleanup(func() {
				_, dropErr := db.Exec(
					context.WithoutCancel(t.Context()), "DROP ROLE "+pgx.Identifier{role}.Sanitize(),
				)
				require.NoError(t, dropErr)
			})
			conn, err := pgx.ConnectConfig(t.Context(), db.Config().ConnConfig.Copy())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close(context.WithoutCancel(t.Context()))) })
			sessions := replacement.PostgresSessions{Conn: conn, Roles: []string{role}}
			names, err := sessions.Names(t.Context())
			require.NoError(t, err)
			require.Empty(t, names)
			var ctx context.Context
			var cancel context.CancelFunc
			if scenario.name == "canceled" {
				ctx, cancel = context.WithCancel(t.Context())
				cancel()
			} else {
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			}
			defer cancel()
			names, err = sessions.Names(ctx)
			require.ErrorIs(t, err, scenario.cause)
			require.Nil(t, names)
			// An interrupted observation must not invent sessions or poison later reads.
			names, err = sessions.Names(t.Context())
			require.NoError(t, err)
			require.Empty(t, names)
		})
	}
}
