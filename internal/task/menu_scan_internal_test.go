package task

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

func menuScanStore(t *testing.T) *db.Store {
	t.Helper()
	dsn := os.Getenv("ZNS_CHATBOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZNS_CHATBOT_TEST_DATABASE_URL is required for real PostgreSQL test")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	schema := "menu_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = pool.Exec(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, dropErr := pool.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, dropErr)
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(store.Close)
	require.NoError(t, store.Migrate(ctx))
	return store
}

func TestMenuScanCancellationPreservesUnprocessedCursor(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	for n := 2; n <= 3; n++ {
		_, err := store.Pool().Exec(t.Context(), `INSERT INTO shuttle_users
            (id,bot_id,tg_user_id,zitadel_org,zitadel_subject) VALUES($1,1,$2,'org',$3)`,
			fmt.Sprintf("00000000-0000-0000-0000-%012d", n), int64(n), fmt.Sprintf("owner%d", n))
		require.NoError(t, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	s := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			cancel()
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	credentials := identity.ClientCredentials{ID: "fixture", Secret: "fixture"}
	iam, err := identity.New(identity.Options{Issuer: "https://iam.invalid", OrganizationID: "org",
		LoomAudience: "loom", APIAudience: "api", ActorID: "actor", BotID: 1,
		ActorClientCredentials: credentials, ExchangeClientCredentials: credentials,
		IntrospectionClientCredentials: credentials}, nil)
	require.NoError(t, err)
	s.store, s.iam = store, iam
	after := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	require.Equal(t, after, s.scanMenuPage(ctx, after), "cancelled page cannot advance past untouched users")
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, uuid.Nil, s.scanMenuPage(t.Context(), after))
	require.Equal(t, int32(4), calls.Load(), "deferred first owner cannot starve the second owner")
}
