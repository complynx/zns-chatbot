package botdelivery

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func storedJSONDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_botdelivery_json_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	return db
}

func storedJSONTx(t *testing.T, db *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	return tx
}

func requireStoredJSONError(t *testing.T, err error) {
	t.Helper()
	var incompatible *json.UnmarshalTypeError
	require.ErrorAs(t, err, &incompatible)
	require.False(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "private-incompatible")
}

func TestStoredPassMenuIncompatibleJSONIsNotDatabaseFailure(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	_, err := db.Exec(t.Context(), `INSERT INTO bot.pass_views(owner,chat_id,revision,state)
 VALUES('alice',101,1,'{"redacted":"private-incompatible"}')`)
	require.NoError(t, err)
	i := Intent{Owner: "alice", Chat: 101, Reference: Reference{Kind: CardIntent, Family: familyPasses, Revision: 1}}
	tx := storedJSONTx(t, db)
	_, err = readPassMenuFamily(t.Context(), tx, i)
	requireStoredJSONError(t, err)
	requireStoredJSONError(t, lockViewBinding(t.Context(), tx, i, familyRead{}))

	missing := Intent{Owner: "bob", Chat: 202, Reference: i.Reference}
	_, err = readPassMenuFamily(t.Context(), tx, missing)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.False(t, core.IsDatabaseFailure(err))
	err = lockViewBinding(t.Context(), tx, missing, familyRead{})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestStoredBotResultIncompatibleJSONIsNotDatabaseFailure(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	_, err := db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('alice',7,'delivery_result:incompatible','{"generation":"private-incompatible"}')`)
	require.NoError(t, err)
	result := StoredResult{Generation: 3, Notice: i18n.IdentityUnavailable}
	intent := func(kind string) Intent {
		return Intent{Owner: "alice", Chat: 101, Reference: Reference{Update: 7, ResultKind: kind}}
	}
	tx := storedJSONTx(t, db)
	requireStoredJSONError(t, storeBotResult(t.Context(), tx, intent("delivery_result:incompatible"), result))

	// Controls: a compatible row keeps the existing replay and stale semantics.
	require.NoError(t, storeBotResult(t.Context(), tx, intent("delivery_result:valid"), result))
	require.NoError(t, storeBotResult(t.Context(), tx, intent("delivery_result:valid"), result))
	result.Generation = 4
	require.ErrorIs(t, storeBotResult(t.Context(), tx, intent("delivery_result:valid"), result), ErrStale)
}

// storedJSONRowTx accepts writes and answers every row read with one synthetic row.
type storedJSONRowTx struct {
	pgx.Tx

	row readDatabaseRow
}

func (storedJSONRowTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (tx storedJSONRowTx) QueryRow(context.Context, string, ...any) pgx.Row { return tx.row }

func TestStoredJSONSyntheticSQLAndDecodeProvenance(t *testing.T) {
	t.Parallel()
	passes := Intent{Owner: "alice", Reference: Reference{Kind: CardIntent, Family: familyPasses, Revision: 1}}
	result := Intent{Owner: "alice", Reference: Reference{Update: 7, ResultKind: "delivery_result:synthetic"}}
	stored := StoredResult{Notice: i18n.IdentityUnavailable}
	for _, failure := range []error{io.EOF, pgx.ErrNoRows, context.Canceled} {
		tx := storedJSONRowTx{row: func(...any) error { return failure }}
		_, readErr := readPassMenuFamily(t.Context(), tx, passes)
		lockErr := lockViewBinding(t.Context(), tx, passes, familyRead{})
		storeErr := storeBotResult(t.Context(), tx, result, stored)
		for _, err := range []error{readErr, lockErr, storeErr} {
			if errors.Is(failure, io.EOF) {
				require.Equal(t, core.ErrDatabase, err)
			} else {
				require.ErrorIs(t, err, failure)
				require.False(t, core.IsDatabaseFailure(err))
			}
		}
	}
	// A successful SQL row is decoded only after the scan into raw bytes.
	tx := storedJSONRowTx{row: func(dest ...any) error {
		*dest[0].(*[]byte) = []byte("{")
		if len(dest) > 1 {
			*dest[1].(*int64) = 1
		}
		return nil
	}}
	_, readErr := readPassMenuFamily(t.Context(), tx, passes)
	lockErr := lockViewBinding(t.Context(), tx, passes, familyRead{})
	storeErr := storeBotResult(t.Context(), tx, result, stored)
	for _, err := range []error{readErr, lockErr, storeErr} {
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
		require.False(t, core.IsDatabaseFailure(err))
	}
}
