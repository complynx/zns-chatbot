package botdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type readDatabaseRow func(...any) error

func (row readDatabaseRow) Scan(dest ...any) error { return row(dest...) }

type readDatabase struct {
	dbgen.DBTX

	row pgx.Row
}

func (db readDatabase) QueryRow(context.Context, string, ...any) pgx.Row { return db.row }

func TestReadDatabaseFailureAndControls(t *testing.T) {
	t.Parallel()
	for _, lock := range []bool{false, true} {
		for _, failure := range []error{io.EOF, pgx.ErrNoRows, context.Canceled, context.DeadlineExceeded} {
			db := readDatabase{row: readDatabaseRow(func(...any) error { return failure })}
			_, err := Read(t.Context(), db, 1, delivery.Reference{}, lock)
			if errors.Is(failure, io.EOF) {
				require.ErrorIs(t, err, core.ErrDatabase)
			} else {
				require.ErrorIs(t, err, failure)
				require.False(t, core.IsDatabaseFailure(err))
			}
		}
	}
}

func TestReadJSONErrorsKeepTheirProvenance(t *testing.T) {
	t.Parallel()
	for _, lock := range []bool{false, true} {
		for _, brokenColumn := range []int{5, 13} {
			db := readDatabase{row: readDatabaseRow(func(dest ...any) error {
				*dest[5].(*[]byte) = []byte("{}")
				*dest[13].(*[]byte) = []byte("{}")
				*dest[brokenColumn].(*[]byte) = []byte("{")
				return nil
			})}
			_, err := Read(t.Context(), db, 1, delivery.Reference{}, lock)
			var syntax *json.SyntaxError
			require.ErrorAs(t, err, &syntax)
			require.False(t, core.IsDatabaseFailure(err))
		}
	}
}
