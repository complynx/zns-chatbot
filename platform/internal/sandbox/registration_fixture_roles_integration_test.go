package sandbox_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

// This suite owns an explicitly allocated fresh cluster with the template
// bootstrap already applied. Never point it at an active functional stand.
func TestRegistrationPrivateRoleComposition(t *testing.T) {
	t.Parallel()
	if os.Getenv("REGISTRATION_ROLE_TEST_ADMIN_URL") == "" {
		t.Skip("allocated fresh registration role cluster required")
	}
	admin := registrationRolePool(t, "ADMIN")
	owner := registrationRolePool(t, "OWNER")
	operator := registrationRolePool(t, "OPERATOR")
	fakeDB := registrationRolePool(t, "FAKE")
	ctx := t.Context()
	require.NoError(t, store.Migrate(ctx, owner))
	require.NoError(t, store.Seed(ctx, owner))
	require.NoError(t, sandbox.ApplyProductFixture(ctx, owner))
	init := sandbox.RegistrationFixture{Stand: sandbox.RegistrationFixtureStand, Action: "init",
		OpensAt: time.Date(2030, 10, 2, 12, 0, 0, 0, time.UTC)}
	_, err := sandbox.ApplyRegistrationFixture(ctx, owner, init)
	require.NoError(t, err)
	acl, err := os.ReadFile("../../../docs/sandbox/fqa-stands/registration/runtime-roles.sql")
	require.NoError(t, err)
	_, err = owner.Exec(ctx, string(acl))
	require.NoError(t, err)
	registrationRoleActions(t, owner, operator, init)
	registrationFakePersistence(t, owner, fakeDB)
	registrationRoleInventory(t, owner, operator, fakeDB)
	registrationAllocationDenials(t, admin, fakeDB)
	registrationRoleDenials(t, admin, operator, fakeDB, init)
}

func registrationRolePool(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	db, err := pgxpool.New(t.Context(), os.Getenv("REGISTRATION_ROLE_TEST_"+role+"_URL"))
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, db.Ping(t.Context()))
	return db
}

func registrationRoleActions(t *testing.T, owner, operator *pgxpool.Pool, init sandbox.RegistrationFixture) {
	t.Helper()
	_, err := sandbox.ApplyRegistrationFixture(t.Context(), operator, init)
	require.ErrorContains(t, err, "cannot initialize")
	for _, action := range []string{"read", "revoke-payment-a", "restore-payment-a", "grant-payment-b",
		"revoke-payment-b", "revoke-booking-admin", "restore-booking-admin"} {
		state, actionErr := sandbox.ApplyRegistrationFixture(t.Context(), operator,
			sandbox.RegistrationFixture{Stand: sandbox.RegistrationFixtureStand, Action: action})
		require.NoError(t, actionErr, action)
		assert.Len(t, state.Rows, 6, action)
	}
	var paymentA, paymentB, booking bool
	err = owner.QueryRow(t.Context(), `SELECT
 EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE event_id='registration-fixture-a' AND owner='bob'),
 EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE event_id='registration-fixture-b' AND owner='bob'),
 EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner='visitor')`).Scan(&paymentA, &paymentB, &booking)
	require.NoError(t, err)
	assert.True(t, paymentA)
	assert.False(t, paymentB)
	assert.True(t, booking)
	_, err = sandbox.ApplyRegistrationFixture(t.Context(), owner,
		sandbox.RegistrationFixture{Stand: sandbox.RegistrationFixtureStand, Action: "read"})
	require.NoError(t, err, "existing owner live path remains available before admission")
}

func registrationFakePersistence(t *testing.T, owner, fakeDB *pgxpool.Pool) {
	t.Helper()
	_, err := owner.Exec(t.Context(), `INSERT INTO bot.cursors(name,value) VALUES('telegram',9)
 ON CONFLICT(name) DO UPDATE SET value=EXCLUDED.value;
 INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',9,'input','{"text":"synthetic"}')
 ON CONFLICT(owner,update_id,kind) DO NOTHING`)
	require.NoError(t, err)
	fake, err := sandbox.New(t.Context(), fakeDB, "synthetic-role-test")
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"/lab/document?user=101&filename=synthetic.txt",
		bytes.NewBufferString("synthetic bytes"),
	)
	request.Header.Set("X-Sandbox", "1")
	recorder := httptest.NewRecorder()
	fake.Handler().ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var fileID string
	err = fakeDB.QueryRow(t.Context(), `SELECT id FROM bot.fake_files WHERE filename='synthetic.txt'`).Scan(&fileID)
	require.NoError(t, err)
	restarted, err := sandbox.New(t.Context(), fakeDB, "synthetic-role-test")
	require.NoError(t, err)
	state := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	require.Equal(t, http.StatusOK, state.Code, state.Body.String())
	assert.Contains(t, state.Body.String(), `"processed_cursor":9`)
	assert.Contains(t, state.Body.String(), `"update_id":9`)
	assert.Contains(t, state.Body.String(), "synthetic.txt")
	file := httptest.NewRecorder()
	restarted.Handler().
		ServeHTTP(file, httptest.NewRequest(http.MethodGet, "/file/botsynthetic-role-test/"+fileID, nil))
	require.Equal(t, http.StatusOK, file.Code)
	body, err := io.ReadAll(file.Body)
	require.NoError(t, err)
	assert.Equal(t, "synthetic bytes", string(body))
}

func registrationRoleInventory(t *testing.T, owner, operator, fakeDB *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	inventory, err := pgx.Connect(ctx, os.Getenv("REGISTRATION_ROLE_TEST_INVENTORY_URL"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inventory.Close(context.WithoutCancel(ctx))) })
	for _, pool := range []*pgxpool.Pool{operator, fakeDB} {
		connection, acquireErr := pool.Acquire(ctx)
		require.NoError(t, acquireErr)
		defer connection.Release()
	}
	owner.Close()
	names, err := (replacement.PostgresSessions{Conn: inventory, Roles: []string{"zns_app", "zns_meter"}}).Names(ctx)
	require.NoError(t, err)
	assert.Empty(t, names, "persistent fake and held operator connections cannot block managed retirement")
}

func registrationRoleDenials(t *testing.T, admin, operator, fakeDB *pgxpool.Pool, init sandbox.RegistrationFixture) {
	t.Helper()
	// Reopen the owner after proving zero managed sessions.
	owner := registrationRolePool(t, "OWNER")
	ctx := t.Context()
	for _, statement := range []string{
		`UPDATE core.users SET can_book=false`,
		`INSERT INTO public.zns_sandbox_fixtures(name) VALUES('forged')`,
		`CREATE TABLE public.operator_created(id int)`,
		`UPDATE core.pass_events SET finishes_at=now()`,
	} {
		_, err := operator.Exec(ctx, statement)
		require.Error(t, err, statement)
	}
	for _, statement := range []string{`SELECT * FROM core.users`, `UPDATE bot.cursors SET value=0`, `DELETE FROM bot.fake_files`} {
		_, err := fakeDB.Exec(ctx, statement)
		require.Error(t, err, statement)
	}
	grant := sandbox.RegistrationFixture{Stand: sandbox.RegistrationFixtureStand, Action: "grant-payment-b"}
	for _, scenario := range []struct{ change, restore string }{
		{`UPDATE core.users SET telegram_id=404 WHERE id='visitor'`, `UPDATE core.users SET telegram_id=303 WHERE id='visitor'`},
		{`INSERT INTO core.users(id,telegram_id,name) VALUES('fourth',404,'synthetic')`, `DELETE FROM core.users WHERE id='fourth'`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='product-v1'`, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES('product-v1')`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='product-passport-v1'`, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES('product-passport-v1')`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='registration-fqa-v1'`, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES('registration-fqa-v1')`},
		{`ALTER TABLE public.zns_sandbox_fixtures OWNER TO postgres`, `ALTER TABLE public.zns_sandbox_fixtures OWNER TO zns_app`},
		{`ALTER TABLE core.users OWNER TO postgres`, `ALTER TABLE core.users OWNER TO zns_app`},
		{`ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO postgres`, `ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO zns_app`},
		{`GRANT zns_app TO zns_registration_operator`, `REVOKE zns_app FROM zns_registration_operator`},
		{`ALTER ROLE zns_registration_operator SUPERUSER`, `ALTER ROLE zns_registration_operator NOSUPERUSER`},
		{`ALTER ROLE zns_registration_operator CREATEDB`, `ALTER ROLE zns_registration_operator NOCREATEDB`},
		{`ALTER ROLE zns_registration_operator CREATEROLE`, `ALTER ROLE zns_registration_operator NOCREATEROLE`},
		{`ALTER ROLE zns_registration_operator REPLICATION`, `ALTER ROLE zns_registration_operator NOREPLICATION`},
		{`ALTER ROLE zns_registration_operator BYPASSRLS`, `ALTER ROLE zns_registration_operator NOBYPASSRLS`},
		{`GRANT pg_read_all_stats TO zns_registration_operator`, `REVOKE pg_read_all_stats FROM zns_registration_operator`},
	} {
		_, err := admin.Exec(ctx, scenario.change)
		require.NoError(t, err)
		_, rejected := sandbox.ApplyRegistrationFixture(ctx, operator, grant)
		_, restoreErr := admin.Exec(ctx, scenario.restore)
		require.NoError(t, restoreErr)
		require.Error(t, rejected, scenario.change)
		var changed bool
		err = owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_payment_admins
 WHERE event_id='registration-fixture-b' AND owner='bob')`).Scan(&changed)
		require.NoError(t, err)
		assert.False(t, changed, "rejected guard must not mutate", scenario.change)
	}
	_, err := sandbox.ApplyRegistrationFixture(ctx, owner, init)
	require.NoError(t, err, "owner init replay remains inert")
}

func registrationAllocationDenials(t *testing.T, admin, fakeDB *pgxpool.Pool) {
	t.Helper()
	read := sandbox.RegistrationFixture{Stand: sandbox.RegistrationFixtureStand, Action: "read"}
	for _, db := range []*pgxpool.Pool{admin, fakeDB} {
		_, err := sandbox.ApplyRegistrationFixture(t.Context(), db, read)
		require.Error(t, err, "unrelated login cannot use fixture controls")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("REGISTRATION_ROLE_TEST_OPERATOR_URL"))
	require.NoError(t, err)
	cfg.ConnConfig.Database = "postgres"
	wrong, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(wrong.Close)
	require.NoError(t, wrong.Ping(t.Context()))
	_, err = sandbox.ApplyRegistrationFixture(t.Context(), wrong, read)
	require.ErrorContains(t, err, "database or login guard failed")
}
