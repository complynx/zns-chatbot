package derivedmutation

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type sourceCommitTx struct {
	nativeSourceTx

	err       error
	committed bool
}

func (tx *sourceCommitTx) Commit(context.Context) error {
	tx.committed = true
	return tx.err
}

type sourcePrepared struct {
	err     error
	replay  bool
	applied bool
}

func (p *sourcePrepared) Replay() (int, bool) { return 7, p.replay }

func (p *sourcePrepared) Apply(context.Context) (int, error) {
	p.applied = true
	return 7, p.err
}

func TestSourceMutationCommitPreservesErrorProvenance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		commitErr error
		applyErr  error
		replay    bool
		want      error
	}{
		{name: "SQL EOF", commitErr: io.EOF, want: core.ErrDatabase},
		{name: "commit canceled", commitErr: context.Canceled, want: context.Canceled},
		{name: "application EOF", applyErr: io.EOF, want: io.EOF},
		{name: "success"},
		{name: "receipt replay", replay: true, commitErr: io.EOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tx := &sourceCommitTx{err: test.commitErr}
			prepared := &sourcePrepared{err: test.applyErr, replay: test.replay}
			generation := int64(0)
			value, err := commitPrepared(t.Context(), tx, "alice",
				readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}, prepared)
			require.ErrorIs(t, err, test.want)
			require.Equal(t, errors.Is(test.want, core.ErrDatabase), core.IsDatabaseFailure(err))
			require.Equal(t, !test.replay, prepared.applied)
			require.Equal(t, !test.replay && test.applyErr == nil, tx.committed)
			if test.want == nil {
				require.Equal(t, 7, value)
			}
		})
	}
}

func TestSourceMutationBeginFailureIsDatabaseFailure(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	tx, err := (Service{DB: db}).beginSourceMutation(t.Context(), []string{"alice"}, readsource.Derivation{})
	require.Nil(t, tx)
	require.Equal(t, core.ErrDatabase, err)
}
