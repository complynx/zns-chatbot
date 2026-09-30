package botdelivery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPassReceiptRetirementDatabaseFailure(t *testing.T) {
	t.Parallel()
	s := Service{}
	i := Intent{Owner: "alice", MessageID: 42}
	require.ErrorIs(t, s.retirePassReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, i, nil, 0), core.ErrDatabase)
	require.ErrorIs(
		t,
		s.retirePassReceipt(t.Context(), receiptSQLFailure{err: context.Canceled}, i, nil, 0),
		core.ErrDatabase,
	)
}

func TestPassMenuDefinitiveDenialKeepsCauseAndDatabaseProvenance(t *testing.T) {
	t.Parallel()
	i := Intent{Reference: Reference{Family: familyPasses, Source: &readsource.Derivation{}}}
	cause := &core.ProblemError{Status: http.StatusConflict, Code: "history_stale"}
	denied := passMenuDenied(i, cause)
	_, typed := errors.AsType[*PassMenuDeniedError](denied)
	require.True(t, typed)
	require.ErrorIs(t, denied, cause)
	problem, ok := errors.AsType[*core.ProblemError](denied)
	require.True(t, ok)
	require.Equal(t, PassMenuDeniedCode, problem.Code, "the HTTP host must preserve definitive denial")
	require.True(t, sourceDenied(denied))
	database := core.DatabaseOperationError(io.EOF)
	_, typed = errors.AsType[*PassMenuDeniedError](passMenuDenied(i, database))
	require.False(t, typed)
	require.ErrorIs(t, passMenuDenied(i, database), core.ErrDatabase)
	_, typed = errors.AsType[*PassMenuDeniedError](ErrStale)
	require.False(t, typed, "an obsolete view binding alone is not a definitive denial")
}

func TestPassPreparationReceiptSQLProvenanceAndCancellation(t *testing.T) {
	t.Parallel()
	s := Service{}
	pending := Intent{Owner: "alice", Target: 42, Phase: phaseEdit,
		Reference: Reference{Family: familyPasses, Source: &readsource.Derivation{}}}
	previous, err := s.previousPassReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, pending)
	require.Nil(t, previous)
	require.ErrorIs(t, err, core.ErrDatabase)
	previous, err = s.previousPassReceipt(t.Context(), receiptSQLFailure{err: context.Canceled}, pending)
	require.Nil(t, previous)
	require.ErrorIs(t, err, core.ErrDatabase, "driver cancellation is not request cancellation")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	previous, err = s.previousPassReceipt(ctx, receiptSQLFailure{err: context.Canceled}, pending)
	require.Nil(t, previous)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPassRetirementRequestCancellationAndSQLProvenance(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := Service{}
	i := Intent{Owner: "alice", MessageID: 42}
	require.ErrorIs(t, s.retirePassReceipt(ctx, receiptSQLFailure{err: context.Canceled}, i, nil, 0), context.Canceled)
	statement := &pgconn.PgError{Code: "40001"}
	require.ErrorIs(
		t,
		s.retirePassReceipt(ctx, receiptSQLFailure{err: statement}, i, nil, 0),
		core.ErrDatabaseSerialization,
	)
}
