package conversation_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func TestAppendFullHistoryIsAtomicPrivateAndReplaySafe(t *testing.T) {
	t.Parallel()
	s := historyDatabase(t)
	original := strings.Repeat("Ж🙂", 2000) + "ORIGINAL-END"
	_, err := s.DB.Exec(
		t.Context(),
		`CREATE FUNCTION core.reject_history_body() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic body persistence failure'; END $$; CREATE TRIGGER reject_history_body BEFORE INSERT ON core.conversation_message_bodies FOR EACH ROW EXECUTE FUNCTION core.reject_history_body()`,
	)
	require.NoError(t, err)
	require.Error(t, s.Append(t.Context(), "alice", "atomic-long", "user", original))
	var count int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key='atomic-long'`).
			Scan(&count),
	)
	assert.Zero(t, count, "body failure must not commit a falsely complete excerpt")
	_, err = s.DB.Exec(t.Context(), `DROP TRIGGER reject_history_body ON core.conversation_message_bodies`)
	require.NoError(t, err)
	require.NoError(t, s.Append(t.Context(), "alice", "atomic-long", "user", original))
	require.NoError(t, s.Append(t.Context(), "alice", "atomic-long", "user", original+"CHANGED"))
	var body, excerpt string
	var id int64
	var omitted bool
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT e.id,e.text,e.omitted,b.body FROM core.conversation_events e JOIN core.conversation_message_bodies b ON b.event_id=e.id WHERE source_key='atomic-long'`).
			Scan(&id, &excerpt, &omitted, &body),
	)
	assert.False(t, omitted)
	assert.True(t, utf8.ValidString(excerpt))
	assert.LessOrEqual(t, len(excerpt), conversation.MaxTextBytes)
	assert.Equal(t, original, body)
	require.NoError(t, s.Append(t.Context(), "alice", "suffix-secret", "user", original+" password: synthetic-private"))
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT e.omitted,e.text,(SELECT count(*) FROM core.conversation_message_bodies b WHERE b.event_id=e.id) FROM core.conversation_events e WHERE source_key='suffix-secret'`).
			Scan(&omitted, &excerpt, &count),
	)
	assert.True(t, omitted)
	assert.Equal(t, "[sensitive text omitted]", excerpt)
	assert.Zero(t, count)
	require.Error(
		t,
		s.Append(t.Context(), "alice", "over-limit", "user", strings.Repeat("x", conversation.MaxBodyBytes+1)),
	)
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key='over-limit'`).
			Scan(&count),
	)
	assert.Zero(t, count)
}
