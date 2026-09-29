package bot

import (
	"errors"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestModernChoiceClaimFencesParentGeneration(t *testing.T) {
	t.Parallel()
	for _, sameUpdate := range []bool{false, true} {
		t.Run(map[bool]string{false: "cross-update", true: "same-update"}[sameUpdate], func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			generation := int64(0)
			parent := agenthost.ScriptRecord{
				HistoryGeneration: 0,
				Calls: []agenthost.ScriptToolRecord{
					{
						ModernChoice: &agenthost.ModernChoiceRecord{Ref: "94001.0.0", HistoryGeneration: &generation},
						Outcome:      agent.ScriptToolResult{Result: []byte(`{}`)},
					},
				},
			}
			records := []agenthost.ScriptRecord{parent}
			_, err := db.Exec(
				t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',94001,'script_runs',$1)`,
				records,
			)
			require.NoError(t, err)
			tx, err := db.Begin(t.Context())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(t.Context()) }()
			update := int64(94002)
			if sameUpdate {
				update = 94001
			}
			err = (agenthost.ScriptStore{StaleError: errors.New("stale read")}).ClaimModernChoice(
				t.Context(),
				tx,
				"alice",
				update,
				records,
				"94001.0.0",
				"94002.0.0",
				1,
			)
			require.Error(t, err, "current-generation admission cannot claim an older parent")
			require.NoError(t, tx.Rollback(t.Context()))
			var child string
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT COALESCE(content->0->'calls'->0->'modern_choice'->>'child','') FROM bot.interactions WHERE update_id=94001`).
					Scan(&child),
			)
			require.Empty(t, child)
			require.Empty(t, records[0].Calls[0].ModernChoice.Child)
		})
	}
}
