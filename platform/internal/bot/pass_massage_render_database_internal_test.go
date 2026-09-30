package bot

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestPassMassageRenderDatabaseOrigins(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	b := &Bot{DB: db}
	_, _, err = b.massageState(t.Context(), "alice")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.ErrorIs(t, b.saveMassageState(t.Context(), "alice", 1, 1, massageView{}), core.ErrDatabase)
	require.ErrorIs(t, b.deliverMassageCard(t.Context(), "alice", telegram.Send{}), core.ErrDatabase)
	_, _, err = b.massageButtons(t.Context(), "alice", 1, []massageChoice{{label: "choice"}})
	require.ErrorIs(t, err, core.ErrDatabase)
	_, _, err = b.passMenuRecord(t.Context(), "alice")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = b.registrationReplyPayload(t.Context(), "alice", 1, telegram.Send{})
	require.ErrorIs(t, err, core.ErrDatabase)
	for _, choices := range [][]passMenuChoice{nil, {{label: "choice"}}} {
		err = b.deliverPassMenu(t.Context(), "alice", 1, telegram.Send{}, choices, interaction.RegistrationMenu{}, nil)
		require.ErrorIs(t, err, core.ErrDatabase)
	}
	require.ErrorIs(t, b.redactPassMenu(t.Context(), "alice", 1, 1, botdelivery.PassMenu{}), core.ErrDatabase)
}

func TestPassMassageRenderDatabaseJSONAndMissingControls(t *testing.T) {
	t.Parallel()
	b := &Bot{DB: foodPendingDatabase(t)}
	massage, revision, err := b.massageState(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, massageHome, massage.View)
	require.Zero(t, revision)
	menu, revision, err := b.passMenuRecord(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, passMenuEvents, menu.View)
	require.Zero(t, revision)
	payload := telegram.Send{Text: "original"}
	reply, err := b.registrationReplyPayload(t.Context(), "alice", 1, payload)
	require.NoError(t, err)
	require.Equal(t, payload, reply)
	require.ErrorIs(t, b.deliverMassageCard(t.Context(), "alice", payload), pgx.ErrNoRows)
	for _, raw := range []string{`{}`, `null`, `"wrong type"`} {
		_, err = b.DB.Exec(t.Context(), `INSERT INTO bot.massage_views(owner,chat_id,revision,state)
 VALUES('alice',1,1,$1) ON CONFLICT(owner) DO UPDATE SET state=$1`, raw)
		require.NoError(t, err)
		_, err = b.DB.Exec(t.Context(), `INSERT INTO bot.pass_views(owner,chat_id,revision,state)
 VALUES('alice',1,1,$1) ON CONFLICT(owner) DO UPDATE SET state=$1`, raw)
		require.NoError(t, err)
		storedMassage, _, massageErr := b.massageState(t.Context(), "alice")
		storedMenu, _, menuErr := b.passMenuRecord(t.Context(), "alice")
		if raw == `"wrong type"` {
			var invalid *json.UnmarshalTypeError
			require.ErrorAs(t, massageErr, &invalid)
			require.ErrorAs(t, menuErr, &invalid)
			require.False(t, core.IsDatabaseFailure(massageErr))
			require.False(t, core.IsDatabaseFailure(menuErr))
		} else {
			require.NoError(t, massageErr)
			require.NoError(t, menuErr)
			require.Empty(t, storedMassage.View, "stored empty JSON must not inherit the missing-row default")
			require.Empty(t, storedMenu.View)
		}
	}
}

func TestPassMassageRenderDatabaseRedactionRollback(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		"begin", "UPDATE bot.pass_views SET state=$4", "DELETE FROM bot.pass_buttons", "commit",
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			generation := int64(0)
			saved := botdelivery.PassMenu{Source: &readsource.Derivation{Generation: &generation}}
			_, err := db.Exec(t.Context(), `INSERT INTO bot.pass_views(owner,chat_id,revision,state)
 VALUES('alice',1,1,$1)`, saved)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `INSERT INTO bot.pass_buttons(owner,token,revision,action)
 VALUES('alice','original',1,'{}')`)
			require.NoError(t, err)
			fault := &receiptDatabaseFault{prefix: stage}
			config := db.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			b := &Bot{DB: broken}
			require.ErrorIs(t, b.redactPassMenu(t.Context(), "alice", 1, 1, saved), core.ErrDatabase)
			require.True(t, fault.fired)
			require.NoError(t, fault.closeError)
			var unchanged botdelivery.PassMenu
			require.NoError(
				t,
				db.QueryRow(t.Context(), "SELECT state FROM bot.pass_views WHERE owner='alice'").Scan(&unchanged),
			)
			require.Equal(t, saved, unchanged)
			var buttons int
			err = db.QueryRow(t.Context(), "SELECT count(*) FROM bot.pass_buttons WHERE owner='alice'").Scan(&buttons)
			require.NoError(t, err)
			require.Equal(t, 1, buttons)
			b.DB = db
			require.NoError(t, b.redactPassMenu(t.Context(), "alice", 1, 2, saved), "newer revision race is benign")
		})
	}
}
