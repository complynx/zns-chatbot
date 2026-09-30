package migrate_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func TestMessagesFullHistoryCLIReplayAndReconcile(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	var database string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT current_database()`).Scan(&database))
	t.Logf("isolated database: %s; existing applyDatabase cleanup drops this database", database)
	us, up, ur := applyInputs(t, `{"_id":"synthetic","bot_id":77,"user_id":101,"print_name":"Synthetic"}`)
	_, err := migrate.ApplyUsers(t.Context(), dsn, us, up, ur, migrate.DefaultLimits())
	require.NoError(t, err)
	var owner string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM core.users WHERE telegram_id=101`).Scan(&owner))
	old := strings.Replace(syntheticMessage, "2035-06-01", "1970-01-01", 1)
	newer := strings.Replace(
		strings.Replace(syntheticMessage, "message-one", "message-new", 1),
		"2035-06-01",
		"2099-12-31",
		1,
	)
	newer = strings.Replace(newer, `"role":"user"`, `"role":"assistant"`, 1)
	private := strings.Replace(
		strings.Replace(old, "message-one", "message-private", 1),
		"ordinary synthetic text",
		"PRIVATE_CANARY",
		1,
	)
	media := strings.Replace(
		strings.Replace(old, "message-one", "message-media", 1),
		"ordinary synthetic text",
		"EXPIRED_MEDIA_CANARY",
		1,
	)
	media = strings.TrimSuffix(media, "}") + `,"media_file_id":"synthetic-expired-media"}`
	stage, plan, resolutions, records, r := messageInputs(t, old, newer, private, media)
	r.Retention, r.RetainFrom, r.RetainUntil = "all", "", ""
	for i := range r.Messages {
		r.Messages[i].Owner = owner
		r.Messages[i].ResolvedAt = "1970-01-01T00:00:00Z"
		r.Messages[i].ArchiveFields = records.Messages[i].Candidate.ArchiveFields
	}
	r.Messages[1].ResolvedAt = "2099-12-31T00:00:00Z"
	r.Messages[2].Disposition, r.Messages[2].Provenance = "omitted", "sensitive"
	r.Messages[3].Disposition, r.Messages[3].Provenance = "omitted", "expired"
	writeMessageResolutions(t, resolutions, r)
	for _, operation := range []string{"apply", "apply", "reconcile"} {
		if operation == "reconcile" {
			_, err = db.Exec(t.Context(), `DROP TABLE migrate_import.message_receipts`)
			require.NoError(t, err)
		}
		output := runMessageCLI(
			t,
			dsn,
			operation,
			"messages",
			"--stage",
			stage,
			"--plan",
			plan,
			"--resolutions",
			resolutions,
		)
		var result struct {
			Summary *migrate.MessageApplySummary `json:"apply_messages"`
		}
		require.NoError(t, json.Unmarshal(output, &result))
		require.NotNil(t, result.Summary)
	}
	summary, err := migrate.ApplyMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Zero(t, summary.Applied)
	require.Equal(t, 4, summary.Reused)
	var events, omitted, credits int
	var earliest, latest time.Time
	var content string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER(WHERE omitted),min(created_at),max(created_at),string_agg(text,'') FROM core.conversation_events`).
			Scan(&events, &omitted, &earliest, &latest, &content),
	)
	require.Equal(t, 4, events)
	require.Equal(t, 2, omitted)
	require.Equal(t, 1970, earliest.Year())
	require.Equal(t, 2099, latest.Year())
	require.Equal(t, strings.Repeat("ordinary synthetic text", 2), content)
	var assistant bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM core.conversation_events WHERE kind='assistant' AND created_at=$1 AND NOT omitted)`, latest).
			Scan(&assistant),
	)
	require.True(t, assistant)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts`).Scan(&credits))
	require.Zero(t, credits)
}
