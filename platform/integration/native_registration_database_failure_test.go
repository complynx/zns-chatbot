package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestNativeRegistrationSQLFailureKeepsPendingEvidenceAndRecovers(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	stop := pauseNativeRegistration(t, f)
	stop()
	_, err := f.db.Exec(t.Context(), `CREATE SEQUENCE core.native_fault_attempt;
CREATE FUNCTION core.reject_native_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM nextval('core.native_fault_attempt');
 RAISE EXCEPTION 'private native completion SQL diagnostic';
END $$;
CREATE TRIGGER reject_native_completion BEFORE UPDATE OF native_outcome ON core.registration_ingress
FOR EACH ROW WHEN (NEW.native_outcome <> '') EXECUTE FUNCTION core.reject_native_completion()`)
	require.NoError(t, err)
	services := notificationFixtureServices(f.db, appservices.Options{})
	err = services.Registration.ResolveRegistrationIntake(t.Context(), "dance")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "private native completion SQL diagnostic")
	var called bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT is_called FROM core.native_fault_attempt`).Scan(&called))
	require.True(t, called, "the resolver must reach the completion SQL failure")
	var pending, admissions int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.registration_ingress WHERE native_event='dance' AND native_owner='alice'
   AND native_payload IS NOT NULL AND native_outcome=''),
 (SELECT count(*) FROM core.registration_intents WHERE owner='alice')`).Scan(&pending, &admissions))
	require.Equal(t, 1, pending, "failed completion must retain the original durable evidence")
	require.Zero(t, admissions, "capturing admission and completing native evidence must roll back together")
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_native_completion ON core.registration_ingress`)
	require.NoError(t, err)
	require.NoError(t, services.Registration.ResolveRegistrationIntake(t.Context(), "dance"))
	first := readRegistrationTurn(t, f.db, "alice")
	require.Equal(t, "captured", first.State)
	require.NoError(t, services.Registration.ResolveRegistrationIntake(t.Context(), "dance"))
	require.Equal(t, first, readRegistrationTurn(t, f.db, "alice"), "replay must not renew or duplicate admission")
	var outcome string
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		`SELECT native_outcome FROM core.registration_ingress WHERE native_event='dance' AND native_owner='alice'`,
	).Scan(&outcome))
	require.Equal(t, "admitted", outcome)
}
