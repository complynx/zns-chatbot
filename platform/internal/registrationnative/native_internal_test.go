package registrationnative

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type failedBindingTx struct{ pgx.Tx }

func (failedBindingTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, io.EOF
}

func TestClassifyMarksSQLTransportFailure(t *testing.T) {
	t.Parallel()
	_, found, err := Classify(t.Context(), failedBindingTx{}, 101, "saved-token")
	require.False(t, found)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotErrorIs(t, err, io.EOF)
	_, found, err = Classify(t.Context(), failedBindingTx{}, 0, "saved-token")
	require.False(t, found)
	require.NoError(t, err, "invalid binding is rejected without SQL")
}
