package integration_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestPassProfileHistoryRedactionReplayAndWindow(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	p := passes.Profile{}
	var command passes.Command
	for i := range 35 {
		command = profileCommand("set", "legal_name", "change-"+strconv.Itoa(i), p)
		command.Value = "PRIVATE NAME " + strconv.Itoa(i)
		if i%2 == 1 {
			command.Origin = "agent"
		}
		var err error
		p, err = s.Execute(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	_, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	history, err := s.History(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, history, 30)
	assert.EqualValues(t, 6, history[0].Version)
	assert.EqualValues(t, 35, history[29].Version)
	for i, change := range history {
		assert.Equal(t, "set", change.Action)
		assert.Equal(t, "legal_name", change.Field)
		assert.False(t, change.At.IsZero())
		if i > 0 {
			assert.Equal(t, history[i-1].Version+1, change.Version)
		}
	}
	assert.Equal(t, "manual", history[29].Origin)
	assert.Equal(t, "agent", history[28].Origin)
	encoded, err := json.Marshal(history)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "PRIVATE")
	assert.NotContains(t, string(encoded), "hash")
	assert.NotContains(t, string(encoded), "change-")
	bob, err := s.History(t.Context(), "bob")
	require.NoError(t, err)
	assert.Empty(t, bob)
	_, err = s.History(t.Context(), "unknown")
	requireCode(t, err, "forbidden")
	_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	readOnly, err := s.History(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, history, readOnly)
}

func TestPassProfileHistoryFailuresAreAtomic(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := passes.Service{DB: db}
	begin := profileCommand("begin", "legal_name", "begin", passes.Profile{})
	p, err := s.Execute(t.Context(), "alice", begin)
	require.NoError(t, err)
	submit := profileCommand("submit", "legal_name", "submit", p)
	submit.Value = "PRIVATE VALUE"
	p, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	p, err = s.Execute(t.Context(), "alice", profileCommand("cancel", "", "cancel", p))
	require.NoError(t, err)
	history, err := s.History(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, history, 3)
	assert.Equal(t, "begin", history[0].Action)
	assert.Equal(t, "submit", history[1].Action)
	assert.Equal(t, "cancel", history[2].Action)
	assert.Empty(t, history[2].Field)
	submit.Key = "stale"
	_, err = s.Execute(t.Context(), "alice", submit)
	requireCode(t, err, "pass_profile_stale")
	_, err = db.Exec(
		t.Context(),
		`CREATE FUNCTION core.reject_profile_history_test() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN RAISE EXCEPTION 'test history failure'; END $$;
	CREATE TRIGGER reject_profile_history_test BEFORE INSERT ON core.pass_profile_history
	FOR EACH ROW EXECUTE FUNCTION core.reject_profile_history_test()`,
	)
	require.NoError(t, err)
	set := profileCommand("set", "legal_name", "atomic", p)
	set.Value = "PRIVATE REPLACEMENT"
	_, err = s.Execute(t.Context(), "alice", set)
	require.Error(t, err)
	unchanged, err := s.Get(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, p, unchanged)
	remaining, err := s.History(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, history, remaining)
	_, err = db.Exec(t.Context(), `DROP TRIGGER reject_profile_history_test ON core.pass_profile_history`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", set)
	require.NoError(t, err)
	remaining, err = s.History(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, remaining, 4)
	assert.Equal(t, p.Version+1, remaining[3].Version)
}
