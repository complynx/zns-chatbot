package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestProfileSQLFailureLocalAndHTTPRecovery(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"local", "HTTP"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			client := f.b.API
			if route == "local" {
				client.LocalRegistration = &appclient.LocalRegistration{
					Profile: passes.Service{DB: f.db},
					Authorizer: applicationauth.Authorizer{
						DB: f.db,
						Verify: func(_ context.Context, token string) (string, error) {
							return f.b.Host.Signer.Verify(token)
						},
					},
				}
				client.HTTP = &http.Client{Transport: directOrderHTTP{t: t}}
			}
			initial, err := client.PassProfile(t.Context(), "alice")
			require.NoError(t, err)
			command := profileCommand("set", "legal_name", "profile-sql-recovery", initial)
			command.Value = "Synthetic Name"
			_, err = f.db.Exec(
				t.Context(),
				`CREATE FUNCTION core.reject_test_profile() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'private profile SQL diagnostic'; END $$;
CREATE TRIGGER reject_test_profile BEFORE INSERT OR UPDATE ON core.pass_profiles
FOR EACH ROW EXECUTE FUNCTION core.reject_test_profile()`,
			)
			require.NoError(t, err)
			_, err = client.ExecutePassProfile(t.Context(), "alice", command)
			require.ErrorIs(t, err, core.ErrDatabase)
			var problem *core.ProblemError
			require.ErrorAs(t, err, &problem)
			require.Equal(t, http.StatusInternalServerError, problem.Status)
			require.NotContains(t, err.Error(), "private profile")
			unchanged, err := client.PassProfile(t.Context(), "alice")
			require.NoError(t, err)
			require.Equal(t, initial, unchanged)
			_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_test_profile ON core.pass_profiles`)
			require.NoError(t, err)
			current, err := client.ExecutePassProfile(t.Context(), "alice", command)
			require.NoError(t, err)
			require.Equal(t, command.Value, current.LegalName)
			replay, err := client.ExecutePassProfile(t.Context(), "alice", command)
			require.NoError(t, err)
			require.Equal(t, current, replay)
			var receipts int
			require.NoError(t, f.db.QueryRow(t.Context(),
				`SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).Scan(&receipts))
			require.Equal(t, 1, receipts)
		})
	}
}
