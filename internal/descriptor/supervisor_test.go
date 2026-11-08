//nolint:testpackage // The retry gate must reject uncertain effects before invoking the loop again.
package descriptor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

func TestSupervisorRejectsUncertainAndPermanentFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		cause error
		calls int
	}{
		{"catalog incompatible", &descriptorCatalogReadError{&pgconn.PgError{Code: "42P01"}}, 1},
		{"uncertain paid action", &loom.Error{Retryable: true, Outcome: loom.Unknown}, 1},
		{"journal corrupt", errors.New("invalid journal"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			err := superviseDescriptor(t.Context(), time.Millisecond, time.Millisecond, func(string) {}, func() error {
				calls++
				return tc.cause
			})
			require.ErrorIs(t, err, tc.cause)
			require.Equal(t, tc.calls, calls)
		})
	}
}

func TestSupervisorRecoversAfterExtendedReadOutage(t *testing.T) {
	t.Parallel()
	calls, retries := 0, 0
	err := superviseDescriptor(t.Context(), time.Microsecond, time.Millisecond, func(state string) {
		if state == "retrying" {
			retries++
		}
	}, func() error {
		calls++
		if calls <= 10 {
			return &descriptorCatalogReadError{&pgconn.PgError{Code: "08006"}}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 11, calls)
	require.Equal(t, 10, retries)
}

func TestFreshCatalogOutageRetriesWithoutBlockingOrReplacingGeneration(t *testing.T) {
	t.Parallel()
	a := arguments{journal: t.TempDir()}
	entry := db.SymbolRecord{Generation: uuid.New(), Reference: symbols.Reference{Kind: symbols.Sticker, ID: "same"}}
	calls := 0
	err := superviseDescriptor(t.Context(), time.Millisecond, time.Millisecond, func(string) {}, func() error {
		_, stepErr := advanceEntry(t.Context(), a, entry, func(_ context.Context, next arguments) error {
			calls++
			var lookupErr error
			if calls == 1 {
				lookupErr = &pgconn.PgError{Code: "08006"}
			}
			path := filepath.Join(next.journal, "journal.json")
			state, readErr := readJournal(next, path, entry, lookupErr)
			if readErr != nil {
				return readErr
			}
			require.Equal(t, entry.Generation, state.Generation)
			state.Phase = descriptorDonePhase
			return save(path, state)
		})
		return stepErr
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	_, err = os.Stat(filepath.Join(a.journal, entry.Generation.String(), "blocked.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSupervisorCancellationInterruptsBackoff(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	err := superviseDescriptor(ctx, time.Hour, time.Hour, func(state string) {
		if state == "retrying" {
			cancel()
		}
	}, func() error {
		calls++
		return &descriptorCatalogReadError{&pgconn.PgError{Code: "08006"}}
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}
