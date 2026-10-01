package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type progressDB struct{ row progressRow }

func (d progressDB) QueryRow(context.Context, string, ...any) pgx.Row { return d.row }

type progressRow struct {
	values [13]int64
	err    error
}

func (r progressRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		*dest[i].(*int64) = value
	}
	return nil
}

func TestJobProgressMetadataAndSQLFailureProvenance(t *testing.T) {
	t.Parallel()
	values := [13]int64{10, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 0}
	progress, err := readJobProgress(t.Context(), progressDB{progressRow{values: values}}, 123)
	require.NoError(t, err)
	require.Equal(t, int64(10), progress.Total)
	require.Equal(t, int64(2), progress.SharedPaused)
	data, err := json.Marshal(progress)
	require.NoError(t, err)
	for _, forbidden := range []string{"destination", "content", "owner", "failure", "id", "123"} {
		require.NotContains(t, string(data), forbidden)
	}
	for _, source := range []error{errors.New("private SQL"), &pgconn.PgError{Code: "40001", Message: "private table"}} {
		progress, err = readJobProgress(t.Context(), progressDB{progressRow{err: source}}, 123)
		require.Equal(t, JobProgress{}, progress)
		require.ErrorIs(t, err, core.ErrDatabase)
		require.NotContains(t, err.Error(), "private")
		if _, ok := errors.AsType[*pgconn.PgError](source); ok {
			require.ErrorIs(t, err, core.ErrDatabaseSerialization)
		}
	}
	values[12] = 1
	_, err = readJobProgress(t.Context(), progressDB{progressRow{values: values}}, 123)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = readJobProgress(ctx, progressDB{progressRow{err: context.Canceled}}, 123)
	require.ErrorIs(t, err, context.Canceled)
}
