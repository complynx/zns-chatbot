package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestTurnFailurePropagatesOnlyDatabaseProvenance(t *testing.T) {
	t.Parallel()
	leaves := turnLeaves{}
	for name, scenario := range map[string]struct {
		err       error
		propagate bool
	}{
		"marker":    {err: core.ErrDatabase, propagate: true},
		"wrapped":   {err: fmt.Errorf("script: %w", core.DatabaseFailure(errors.New("orders unavailable"))), propagate: true},
		"pg":        {err: &pgconn.PgError{Message: "private sql detail"}, propagate: true},
		"joined":    {err: errors.Join(errors.New("stale source"), core.ErrDatabase), propagate: true},
		"authority": {err: errPassAuthorityUnavailable, propagate: true},
		"http500":   {err: &core.ProblemError{Status: http.StatusInternalServerError, Code: "internal"}},
		"domain":    {err: &core.ProblemError{Status: http.StatusConflict, Code: "conflict"}},
		"eof":       {err: io.EOF},
		"generic":   {err: errors.New("provider unavailable")},
		// Turn.Plan returns cancellation itself; Failure does not reclassify it.
		"canceled": {err: fmt.Errorf("script: %w", context.Canceled)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, propagate := leaves.Failure(scenario.err)
			require.Equal(t, scenario.propagate, propagate)
		})
	}
}
