package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRegistrationUpdateExactOrderedAdmissions(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := r96Admission(t, "z-first", "selected-original")
	second := r96Admission(t, "a-second", "second-original")
	third := r96Admission(t, "m-third", "third-original")
	r96StoreAdmissions(t, f, "alice", 96000, []agenthost.ScriptRecord{
		{Calls: []agenthost.ScriptToolRecord{{}, first, second}},
		{PassRedacted: true, Calls: []agenthost.ScriptToolRecord{third}},
	})
	older := r96Admission(t, first.Pass.ID, "old-update-original")
	r96StoreAdmissions(t, f, "alice", 95999, []agenthost.ScriptRecord{{Calls: []agenthost.ScriptToolRecord{older}}})
	foreign := r96Admission(t, first.Pass.ID, "foreign-owner-original")
	foreign.Pass.Witness.Owner = "bob"
	r96StoreAdmissions(t, f, "bob", 96000, []agenthost.ScriptRecord{{Calls: []agenthost.ScriptToolRecord{foreign}}})
	for index := range 25 {
		call := r96Admission(t, fmt.Sprintf("noise-%02d", index), fmt.Sprintf("noise-key-%02d", index))
		r96StoreAdmissions(t, f, "alice", 96001+int64(index),
			[]agenthost.ScriptRecord{{Calls: []agenthost.ScriptToolRecord{call}}})
	}
	store := agenthost.ScriptStore{DB: f.db}
	got, err := store.ReadRegistrationOperationsForUpdate(t.Context(), "alice", 96000)
	require.NoError(t, err)
	require.Len(t, got, 3)
	for index, expected := range []agenthost.ScriptToolRecord{first, second, third} {
		require.Equal(t, expected.Pass.ID, got[index].ID)
		require.Equal(t, expected.Pass.Name, got[index].Tool)
		require.Equal(t, expected.Pass.Command, got[index].Command)
		require.Equal(t, expected.Pass.Witness, got[index].Witness)
		require.Equal(t, expected.Source, got[index].Source)
		require.Equal(t, index == 2, got[index].Retired)
		require.True(t, time.Unix(96000, 0).Equal(got[index].AdmittedAt))
	}
	other, err := store.ReadRegistrationOperationsForUpdate(t.Context(), "bob", 96000)
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.Equal(t, foreign.Pass.Command, other[0].Command)
	missing, err := store.ReadRegistrationOperationsForUpdate(t.Context(), "alice", 95998)
	require.NoError(t, err)
	require.Empty(t, missing)
	legacy, err := store.ReadRegistrationOperations(t.Context(), "alice", first.Pass.ID)
	require.NoError(t, err)
	require.Len(t, legacy, 1)
	require.Equal(t, older.Pass.Command, legacy[0].Command, "legacy exact ID keeps oldest-admission deduplication")
	recent, err := store.ReadRegistrationOperations(t.Context(), "alice", "")
	require.NoError(t, err)
	require.Len(t, recent, 20)
	for index, item := range recent {
		require.Equal(t, fmt.Sprintf("noise-%02d", 24-index), item.ID)
	}
}

func TestRegistrationUpdateBoundAndAmbiguity(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"maximum", "overflow", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			r96AdmissionBoundCase(t, mode)
		})
	}
}

func r96AdmissionBoundCase(t *testing.T, mode string) {
	t.Helper()
	f := setup(t)
	runs := make([]agenthost.ScriptRecord, agent.MaxScriptRuns)
	for run := range runs {
		for call := range agenthost.MaxScriptCalls {
			id := fmt.Sprintf("run-%d-call-%d", run, call)
			runs[run].Calls = append(runs[run].Calls, r96Admission(t, id, id))
		}
	}
	switch mode {
	case "overflow":
		runs[1].Calls = append(runs[1].Calls, r96Admission(t, "overflow", "overflow"))
	case "duplicate":
		runs[1].Calls[0].Pass.ID = runs[0].Calls[0].Pass.ID
	}
	r96StoreAdmissions(t, f, "alice", 96000, runs)
	got, err := (agenthost.ScriptStore{DB: f.db}).ReadRegistrationOperationsForUpdate(t.Context(), "alice", 96000)
	if mode == "maximum" {
		require.NoError(t, err)
		require.Len(t, got, agent.MaxScriptRuns*agenthost.MaxScriptCalls)
		return
	}
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	require.Nil(t, got)
	if mode == "duplicate" {
		require.EqualError(t, err, "ambiguous registration operation admission")
	} else {
		require.EqualError(t, err, "registration operation admission limit exceeded")
	}
}

func TestRegistrationUpdateJSONBoundary(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", "null", "{}", `{"generation":"invalid"}`} {
		t.Run("source="+source, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			call := map[string]json.RawMessage{"pass": json.RawMessage(`{"id":"original","name":"passes.export"}`)}
			if source != "" {
				call["source"] = json.RawMessage(source)
			}
			raw, err := json.Marshal([]any{map[string]any{"calls": []any{call}}})
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('alice',96000,'script_runs',$1)`, raw)
			require.NoError(t, err)
			got, err := (agenthost.ScriptStore{DB: f.db}).ReadRegistrationOperationsForUpdate(
				t.Context(),
				"alice",
				96000,
			)
			if source == `{"generation":"invalid"}` {
				var malformed *json.UnmarshalTypeError
				require.ErrorAs(t, err, &malformed)
				require.False(t, core.IsDatabaseFailure(err))
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			if source == "{}" {
				require.Equal(t, &readsource.Derivation{}, got[0].Source)
			} else {
				require.Nil(t, got[0].Source)
			}
		})
	}
}

func r96Admission(t *testing.T, id, key string) agenthost.ScriptToolRecord {
	t.Helper()
	command := passbooking.Command{Name: "solo", Event: "dance", Key: key}
	witness, err := passbooking.CommandOperationWitness("alice", command)
	require.NoError(t, err)
	generation := int64(0)
	return agenthost.ScriptToolRecord{
		Pass: &agenthost.ScriptPassRequest{
			ID: id, Name: "passes.registration.solo", Command: &command, Witness: &witness,
		},
		Source: &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
	}
}

func r96StoreAdmissions(t *testing.T, f *fixture, owner string, update int64, runs []agenthost.ScriptRecord) {
	t.Helper()
	raw, err := json.Marshal(runs)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content,created_at)
VALUES($1,$2,'script_runs',$3,$4)`, owner, update, raw, time.Unix(update, 0))
	require.NoError(t, err)
}
