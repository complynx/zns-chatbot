package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRegistrationAdmissionsJSONSQLBoundary(t *testing.T) {
	t.Parallel()
	for _, test := range []r97AdmissionJSONCase{
		{"absent source", `{"id":"receipt","name":"passes.export"}`, "", 1, false},
		{"null source", `{"id":"receipt","name":"passes.export"}`, "null", 1, false},
		{"empty source", `{"id":"receipt","name":"passes.export"}`, "{}", 1, false},
		{"valid source", `{"id":"receipt","name":"passes.export"}`, `{"generation":0,"authorities":[]}`, 1, false},
		{"null request", "null", "null", 0, false},
		{"empty request", "{}", "null", 0, false},
		{"request wrong type", `{"id":{},"name":"passes.export"}`, "null", 0, true},
		{"source wrong type", `{"id":"receipt","name":"passes.export"}`, `{"generation":"private-invalid"}`, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r97AdmissionsJSON(t, test)
		})
	}
}

type r97AdmissionJSONCase struct {
	name       string
	request    string
	source     string
	count      int
	decodeFail bool
}

func r97AdmissionsJSON(t *testing.T, test r97AdmissionJSONCase) {
	t.Helper()
	f := setup(t)
	call := map[string]json.RawMessage{"pass": json.RawMessage(test.request)}
	if test.source != "" {
		call["source"] = json.RawMessage(test.source)
	}
	calls := []map[string]json.RawMessage{call}
	if test.decodeFail {
		calls = append(calls, map[string]json.RawMessage{
			"pass": json.RawMessage(`{"id":"a-valid","name":"passes.export"}`),
		})
	}
	raw, err := json.Marshal([]any{map[string]any{"calls": calls}})
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('alice',971,'script_runs',$1)`, raw)
	require.NoError(t, err)
	got, err := (agenthost.ScriptStore{DB: f.db}).ReadRegistrationOperations(t.Context(), "alice", "")
	if test.decodeFail {
		var decode *json.UnmarshalTypeError
		require.ErrorAs(t, err, &decode)
		require.False(t, core.IsDatabaseFailure(err))
		require.Nil(t, got, "no partial list is exposed alongside invalid JSON")
		return
	}
	require.NoError(t, err)
	require.Len(t, got, test.count)
	if test.count == 0 {
		return
	}
	require.Equal(t, "receipt", got[0].ID)
	switch test.name {
	case "absent source", "null source":
		require.Nil(t, got[0].Source)
	case "empty source":
		require.Equal(t, &readsource.Derivation{}, got[0].Source)
	case "valid source":
		require.NotNil(t, got[0].Source)
		require.True(t, got[0].Source.Valid())
	}
}

func TestRetiredPassOperationSQLBoundaries(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"begin EOF", "begin deadline", "begin cancel", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db, registration := adminPairFixture(t)
			healthy := derivedmutation.Service{DB: db, Registration: registration}
			generation := int64(0)
			input := retiredWitnessFixture(t, healthy, "command", readsource.Derivation{
				Generation: &generation, PrivateHistory: true, Authorities: []readsource.Authority{},
			})
			before, err := healthy.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, err)
			require.Equal(t, "committed", before.Summary.Status)
			stored := r97ReceiptSnapshot(t, db)
			faulty := healthy
			trace := &scriptSQLTrace{statement: "commit"}
			var attempts *atomic.Int32
			if stage == "commit" {
				faulty.DB = scriptSQLPool(t, &fixture{db: db}, trace)
			} else {
				failure := io.EOF
				switch stage {
				case "begin deadline":
					failure = context.DeadlineExceeded
				case "begin cancel":
					failure = context.Canceled
				}
				faulty.DB, attempts = r97ReceiptFaultPool(t, db, failure)
			}
			got, err := faulty.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, t.Context().Err())
			require.Equal(t, core.ErrDatabase, err)
			require.Equal(t, derivedmutation.PassOperationRead{}, got)
			if stage == "commit" {
				requireScriptLocalSQL(t, trace, err)
			} else {
				require.Positive(t, attempts.Load())
			}
			require.Equal(t, stored, r97ReceiptSnapshot(t, db))
			after, err := healthy.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, err)
			require.Equal(t, before, after, "recovery reads the original receipt, without replay")
			require.Equal(t, stored, r97ReceiptSnapshot(t, db))
			r97ReceiptNegativeControls(t, faulty, input)
			_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob';
DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
			require.NoError(t, err)
			denied, err := healthy.ReadPassOperation(t.Context(), "bob", input)
			require.Error(t, err)
			require.False(t, core.IsDatabaseFailure(err))
			require.Equal(t, derivedmutation.PassOperationRead{}, denied)
			require.Equal(t, stored, r97ReceiptSnapshot(t, db))
		})
	}
}

func r97ReceiptFaultPool(t *testing.T, db *pgxpool.Pool, failure error) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config := db.Config().Copy()
	attempts := new(atomic.Int32)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		attempts.Add(1)
		return fmt.Errorf("private receipt connection diagnostic: %w", failure)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, attempts
}

func r97ReceiptSnapshot(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var stored string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT coalesce(jsonb_agg(v ORDER BY v::text),'[]'::jsonb)::text
FROM (SELECT to_jsonb(o) v FROM core.pass_booking_operations o) receipts`).Scan(&stored))
	return stored
}

func r97ReceiptNegativeControls(
	t *testing.T,
	service derivedmutation.Service,
	input derivedmutation.PassOperationInput,
) {
	t.Helper()
	missing := input
	missing.Witness = nil
	got, err := service.ReadPassOperation(t.Context(), "bob", missing)
	requireCode(t, err, "pass_operation_unavailable")
	require.False(t, core.IsDatabaseFailure(err))
	require.Equal(t, derivedmutation.PassOperationRead{}, got)
	got, err = service.ReadPassOperation(t.Context(), "alice", input)
	requireCode(t, err, "pass_operation_unavailable")
	require.False(t, core.IsDatabaseFailure(err))
	require.Equal(t, derivedmutation.PassOperationRead{}, got)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err = service.ReadPassOperation(ctx, "bob", input)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
	require.Equal(t, derivedmutation.PassOperationRead{}, got)
}
