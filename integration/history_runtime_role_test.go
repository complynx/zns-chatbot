package integration_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestHistoryApplyWorksWithoutPermanentSchemaCreate(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	ctx := context.Background()
	var superuser bool
	require.NoError(
		t,
		h.store.Pool().QueryRow(ctx, "SELECT rolsuper FROM pg_roles WHERE rolname=current_user").Scan(&superuser),
	)
	if !superuser {
		t.Skip("restricted-role fixture requires a superuser on the disposable test database")
	}
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	raw, digest := historyBundle(t, "runtime-permission", map[string][]map[string]any{"users": {historyUser(42)}})
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	roleName := "history_" + uuid.NewString()
	role := pgx.Identifier{roleName}.Sanitize()
	config := h.store.Pool().Config().ConnConfig
	schema := pgx.Identifier{config.RuntimeParams["search_path"]}.Sanitize()
	database := pgx.Identifier{config.Database}.Sanitize()
	_, err := h.store.Pool().Exec(ctx, "CREATE ROLE "+role+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := h.store.Pool().Exec(context.Background(), "DROP OWNED BY "+role)
		require.NoError(t, cleanupErr)
		_, cleanupErr = h.store.Pool().Exec(context.Background(), "DROP ROLE "+role)
		require.NoError(t, cleanupErr)
	})
	for _, grant := range []string{
		"GRANT CONNECT,TEMPORARY ON DATABASE " + database + " TO " + role,
		"GRANT USAGE ON SCHEMA " + schema + " TO " + role,
		"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + role,
	} {
		_, err = h.store.Pool().Exec(ctx, grant)
		require.NoError(t, err)
	}
	parsed, err := url.Parse(config.ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("role", roleName)
	parsed.RawQuery = query.Encode()
	runtime, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	var permanentCreate, temporaryCreate bool
	require.NoError(
		t,
		runtime.Pool().
			QueryRow(ctx, "SELECT has_schema_privilege(current_user,$1,'CREATE'),has_database_privi" +
"lege(current_user,current_database(),'TEMPORARY')", config.RuntimeParams["search_path"]).
			Scan(&permanentCreate, &temporaryCreate),
	)
	require.False(t, permanentCreate)
	require.True(t, temporaryCreate)
	receipt, err := runtime.ApplyHistory(
		ctx,
		scope,
		prepareHistory(t, runtime, scope, a),
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, receipt.Inserted)
	page, err := runtime.OwnerHistory(ctx, scope, uuid.Nil, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
}
