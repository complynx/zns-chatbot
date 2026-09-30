package derivedmutation

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedAssignmentBeginSQLFailure(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	_, err = (Service{DB: db}).AssignPass(t.Context(), "bob", passbooking.AdminAssignment{Event: "dance"}, source)
	require.Equal(t, core.ErrDatabase, err)

	_, err = (Service{}).AssignPass(t.Context(), "bob", passbooking.AdminAssignment{}, readsource.Derivation{})
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "invalid_derivation", problem.Code)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestDerivedRegistrationAdmissionBeginSQLFailure(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	request := passbooking.AdmissionRequest{Command: passbooking.Command{Name: "solo", Event: "dance"}}
	_, err = (Service{DB: db}).CapturePassAdmission(t.Context(), "alice", request, source)
	require.Equal(t, core.ErrDatabase, err)
	_, err = (Service{}).CapturePassAdmission(t.Context(), "alice", request, readsource.Derivation{})
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "invalid_derivation", problem.Code)
	require.False(t, core.IsDatabaseFailure(err))
	_, err = (Service{}).CapturePassAdmission(t.Context(), "alice", passbooking.AdmissionRequest{}, source)
	require.NoError(t, err, "non-registration commands must not open an admission transaction")
}
