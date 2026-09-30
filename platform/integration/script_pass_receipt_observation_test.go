package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRetiredInviteReceiptCurrentOwnerObservation(t *testing.T) {
	t.Parallel()
	scenarios := []string{
		"repeated", "deleted-history", "revoked", "sql-failure", "missing-receipt",
		"forged-witness", "missing-witness", "uncommitted", "foreign",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, admitted, retiredJSON := retiredReceiptFixture(t)
			domain := derivedmutation.Service{DB: f.db, Registration: passbooking.Service{DB: f.db}}
			operations := interaction.RegistrationOperations{Ledger: agenthost.ScriptStore{DB: f.db}, Domain: domain}
			fault := prepareReceiptScenario(t, f, scenario, &admitted, retiredJSON, &operations, domain)
			original := receiptAdmissionSnapshot(t, admitted)
			switch scenario {
			case "revoked", "sql-failure", "missing-receipt", "foreign":
				owner := "alice"
				if scenario == "foreign" {
					owner = "bob"
				}
				value, denied := operations.ObserveReceipt(t.Context(), owner, admitted.ID)
				require.Error(t, denied)
				require.Equal(t, interaction.RegistrationReceiptObservation{}, value)
				if fault != nil {
					require.True(t, fault.fired)
					require.NoError(t, fault.closeErr)
					require.ErrorIs(t, denied, core.ErrDatabase)
				}
			default:
				assertRepeatedReceiptObservation(t, operations, admitted)
			}
			assertReceiptObservationNoReplay(t, f, admitted.ID, original, scenario)
		})
	}
}

func retiredReceiptFixture(t *testing.T) (*fixture, interaction.RegistrationOperation, []byte) {
	t.Helper()
	f := passMenuFixture(t)
	first := runPassVM(t, f, 19800, 101, "Invite 202", `
 await tools.passes.registration.read({event:"dance"});
 return tools.passes.registration.invite({event:"dance",invite_telegram_id:202});`)
	require.Empty(t, first.Error)
	var receipt struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(first.Result, &receipt))
	require.True(t, receipt.Complete)
	registration := passbooking.Service{DB: f.db}
	current, err := registration.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = registration.Execute(t.Context(), "alice", bookingCommand("cancel", "receipt-later-cancel", current))
	require.NoError(t, err)
	// The next request reconciles stale history through the actual host path.
	code := fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, receipt.ID)
	replayed := runPassVM(t, f, 19801, 101, "Resume my action", code)
	require.Empty(t, replayed.Error)
	store := agenthost.ScriptStore{DB: f.db}
	admitted, err := store.ReadRegistrationOperations(t.Context(), "alice", receipt.ID)
	require.NoError(t, err)
	require.Len(t, admitted, 1)
	require.True(t, admitted[0].Retired)
	require.NotNil(t, admitted[0].Witness)
	require.NotNil(t, admitted[0].Source)
	var retiredJSON []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=19800 AND kind='script_runs'`).Scan(&retiredJSON))
	var retired []agenthost.ScriptRecord
	require.NoError(t, json.Unmarshal(retiredJSON, &retired))
	require.NotEmpty(t, retired)
	for _, record := range retired {
		require.Empty(t, record.Request.Code)
		require.Empty(t, record.Run.Code)
		for _, call := range record.Calls {
			require.Empty(t, call.Outcome.Result)
		}
	}
	request, source, err := store.ReadRegistrationOperation(t.Context(), "alice", receipt.ID)
	require.Error(t, err)
	require.Nil(t, request)
	require.Nil(t, source)
	return f, admitted[0], retiredJSON
}

func prepareReceiptScenario(
	t *testing.T, f *fixture, scenario string, admitted *interaction.RegistrationOperation,
	retiredJSON []byte, operations *interaction.RegistrationOperations, domain derivedmutation.Service,
) *renderReadFailure {
	t.Helper()
	switch scenario {
	case "sql-failure":
		fault := &renderReadFailure{query: "SELECT request_hash FROM core.pass_booking_operations"}
		config := f.db.Config()
		config.ConnConfig.Tracer = fault
		broken, err := pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(broken.Close)
		domain.DB = broken
		operations.Domain = domain
		return fault
	case "deleted-history":
		deleted, err := agenthost.RedactHistoryInteraction("script_runs", retiredJSON, 7)
		require.NoError(t, err)
		_, err = f.db.Exec(t.Context(), `UPDATE bot.interactions SET content=$1::jsonb
 WHERE owner='alice' AND update_id=19800 AND kind='script_runs'`, string(deleted))
		require.NoError(t, err)
		after, err := operations.Ledger.ReadRegistrationOperations(t.Context(), "alice", admitted.ID)
		require.NoError(t, err)
		require.Len(t, after, 1)
		*admitted = after[0]
	case "revoked":
		_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
		require.NoError(t, err)
	case "missing-receipt":
		_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_operations
 WHERE actor=$1 AND event_id=$2 AND request_hash=$3`, "alice", "dance", admitted.Witness.Digest)
		require.NoError(t, err)
	case "forged-witness", "missing-witness", "uncommitted":
		assertReceiptWitnessDenied(t, domain, *admitted, scenario)
	}
	return nil
}

func assertReceiptWitnessDenied(
	t *testing.T, domain derivedmutation.Service, admitted interaction.RegistrationOperation, scenario string,
) {
	t.Helper()
	command := *admitted.Command
	witness := *admitted.Witness
	input := derivedmutation.PassOperationInput{
		Command: &command, Witness: &witness, Retired: true, ObserveOwnerBooking: true,
	}
	switch scenario {
	case "uncommitted":
		command.Key = "never-committed-owner-invite"
		var err error
		witness, err = passbooking.CommandOperationWitness("alice", command)
		require.NoError(t, err)
	case "forged-witness":
		witness.Digest = strings.Repeat("0", 64)
	case "missing-witness":
		input.Witness = nil
	}
	value, denied := domain.ReadPassOperation(t.Context(), "alice", input)
	require.Error(t, denied)
	require.Nil(t, value.CurrentBooking)
	require.Empty(t, value.ReadAuthorities)
}

func assertRepeatedReceiptObservation(
	t *testing.T, operations interaction.RegistrationOperations, admitted interaction.RegistrationOperation,
) {
	t.Helper()
	for range 3 {
		value, err := operations.ObserveReceipt(t.Context(), "alice", admitted.ID)
		require.NoError(t, err)
		require.Equal(t, admitted.ID, value.ID)
		require.True(t, value.Complete)
		require.Equal(t, "cancelled", value.Result.State)
		require.NotEmpty(t, value.ReadAuthorities)
		visible, err := json.Marshal(agenthost.ModelToolEvidence(value))
		require.NoError(t, err)
		for _, private := range []string{
			"read_authorities", "tg-script", admitted.Witness.Digest, admitted.Witness.Key,
		} {
			require.NotContains(t, string(visible), private)
		}
	}
}

func assertReceiptObservationNoReplay(t *testing.T, f *fixture, id string, original []byte, scenario string) {
	t.Helper()
	store := agenthost.ScriptStore{DB: f.db}
	after, err := store.ReadRegistrationOperations(t.Context(), "alice", id)
	require.NoError(t, err)
	require.Len(t, after, 1)
	encoded := receiptAdmissionSnapshot(t, after[0])
	require.JSONEq(t, string(original), string(encoded), "observation must not rewrite retirement or source identity")
	current, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, "cancelled", current.State)
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).Scan(&count))
	expected := 2
	if scenario == "missing-receipt" {
		expected = 1
	}
	require.Equal(t, expected, count)
}

// The test snapshot includes every admission field without changing the host DTO
// or making private admission metadata part of any model/user projection.
func receiptAdmissionSnapshot(t *testing.T, admitted interaction.RegistrationOperation) []byte {
	t.Helper()
	type menuSnapshot struct {
		Event      string `json:"event"`
		Historical bool   `json:"historical"`
	}
	snapshot := struct {
		ID         string                        `json:"id"`
		Tool       string                        `json:"tool"`
		AdmittedAt time.Time                     `json:"admitted_at"`
		Command    *passbooking.Command          `json:"command"`
		Assignment *passbooking.AdminAssignment  `json:"assignment"`
		Batch      *passbooking.RuntimeBatch     `json:"batch"`
		Menu       *menuSnapshot                 `json:"menu"`
		Source     *readsource.Derivation        `json:"source"`
		Witness    *passbooking.OperationWitness `json:"witness"`
		Retired    bool                          `json:"retired"`
	}{
		ID: admitted.ID, Tool: admitted.Tool, AdmittedAt: admitted.AdmittedAt,
		Command: admitted.Command, Assignment: admitted.Assignment, Batch: admitted.Batch,
		Source: admitted.Source, Witness: admitted.Witness, Retired: admitted.Retired,
	}
	if admitted.Menu != nil {
		snapshot.Menu = &menuSnapshot{Event: admitted.Menu.Event, Historical: admitted.Menu.Historical}
	}
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	return encoded
}
