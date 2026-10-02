package migrate_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func TestMessagesApplyRealUserPipeline(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	us, up, ur := applyInputs(
		t,
		`{"_id":"synthetic","bot_id":77,"user_id":101,"print_name":"Synthetic","legal_name":"Synthetic Person","passport_number":"FAKE"}`,
	)
	runMessageCLI(t, dsn, "apply", "users", "--stage", us, "--plan", up, "--resolutions", ur)
	var err error
	var owner string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM core.users WHERE telegram_id=101`).Scan(&owner))
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events`).Scan(&count))
	require.Zero(t, count)
	body := strings.Repeat("Ю界😀", 2000) + "TERMINAL_MARKER"
	first := strings.Replace(
		strings.Replace(syntheticMessage, "message-one", "z-last", 1),
		"ordinary synthetic text",
		body,
		1,
	)
	second := strings.Replace(syntheticMessage, "message-one", "a-first", 1)
	stage, plan, resolutions, _, r := messageInputs(t, first, second)
	for i := range r.Messages {
		r.Messages[i].Owner = owner
	}
	writeMessageResolutions(t, resolutions, r)
	output := runMessageCLI(t, dsn, "apply", "messages", "--stage", stage, "--plan", plan, "--resolutions", resolutions)
	var response struct {
		Summary migrate.MessageApplySummary `json:"apply_messages"`
	}
	require.NoError(t, json.Unmarshal(output, &response))
	summary := response.Summary
	require.NoError(t, err)
	require.Equal(t, 2, summary.Applied)
	var stored string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT body FROM core.conversation_message_bodies`).Scan(&stored))
	require.Equal(t, body, stored)
	var firstText string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT text FROM core.conversation_events ORDER BY id LIMIT 1`).Scan(&firstText),
	)
	require.Equal(t, "ordinary synthetic text", firstText)
	summary, err = migrate.ApplyMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Zero(t, summary.Applied)
	require.Equal(t, 2, summary.Reused)
	_, err = db.Exec(t.Context(), `DROP TABLE migrate_import.message_receipts`)
	require.NoError(t, err)
	summary, err = migrate.ReconcileMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.True(t, summary.Reconciled)
	summary, err = migrate.ReconcileMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Zero(t, summary.Applied)
	_, err = db.Exec(t.Context(), `UPDATE core.conversation_events SET origin='derived'`)
	require.NoError(t, err)
	_, err = migrate.ReconcileMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "message_content_conflict")
	_, err = db.Exec(t.Context(), `UPDATE core.conversation_events SET origin='original'`)
	require.NoError(t, err)
	// Exercise the persisted terminal state; runtime deletion itself belongs to
	// the runtime lane and is independently tested there.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.conversation_events SET text='',omitted=true,omission_reason='deleted'; DELETE FROM core.conversation_message_bodies; UPDATE core.legacy_message_references SET tombstoned=true`,
	)
	require.NoError(t, err)
	summary, err = migrate.ReconcileMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, 2, summary.Tombstoned)
	require.Zero(t, summary.Applied)
	r.Messages[0].Owner = "changed-owner"
	writeMessageResolutions(t, resolutions, r)
	_, err = migrate.ReconcileMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.Error(t, err)
}
func TestMessagesPreexistingHistoryBlocks(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	us, up, ur := applyInputs(t, `{"_id":"synthetic","bot_id":77,"user_id":101,"print_name":"Synthetic"}`)
	runMessageCLI(t, dsn, "apply", "users", "--stage", us, "--plan", up, "--resolutions", ur)
	var err error
	var owner string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM core.users WHERE telegram_id=101`).Scan(&owner))
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.conversation_events(owner,source_key,kind,text) VALUES($1,'runtime:1','user','later runtime text')`,
		owner,
	)
	require.NoError(t, err)
	stage, plan, resolutions, _, r := messageInputs(t, syntheticMessage)
	r.Messages[0].Owner = owner
	writeMessageResolutions(t, resolutions, r)
	_, err = migrate.ApplyMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "message_preexisting_history")
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.legacy_message_references`).Scan(&count))
	require.Zero(t, count)
}

func TestMessagesOwnerConflictRollsBackWholeHistory(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	us, up, ur := applyInputs(t, `{"_id":"one","bot_id":77,"user_id":101,"print_name":"One"}`)
	_, err := migrate.ApplyUsers(t.Context(), dsn, us, up, ur, migrate.DefaultLimits())
	require.NoError(t, err)
	var owner string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM core.users WHERE telegram_id=101`).Scan(&owner))
	second := strings.Replace(syntheticMessage, "message-one", "message-two", 1)
	second = strings.Replace(second, `"user_id":101`, `"user_id":102`, 1)
	stage, plan, resolutions, _, r := messageInputs(t, syntheticMessage, second)
	r.Messages[0].Owner = owner
	r.Messages[1].Owner = "unknown-owner"
	writeMessageResolutions(t, resolutions, r)
	summary, err := migrate.ApplyMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "message_owner_mismatch")
	require.Zero(t, summary.Applied)
	require.False(t, summary.Reconciled)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.conversation_events)+(SELECT count(*) FROM core.legacy_message_references)+(SELECT count(*) FROM core.conversation_message_bodies)`).Scan(&count))
	require.Zero(t, count)
}

func runMessageCLI(t *testing.T, dsn string, arguments ...string) []byte {
	t.Helper()
	args := append([]string{"run", "./cmd/zns-migrate"}, arguments...)
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Env = append(os.Environ(), "MIGRATE_DATABASE_URL="+dsn)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return output
}

func TestMessagesPrivacyExclusionsAndMissingHistory(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	us, up, ur := applyInputs(
		t,
		`{"_id":"one","bot_id":77,"user_id":101,"print_name":"One"}`,
		`{"_id":"two","bot_id":77,"user_id":102,"print_name":"Two"}`,
	)
	_, err := migrate.ApplyUsers(t.Context(), dsn, us, up, ur, migrate.DefaultLimits())
	require.NoError(t, err)
	var one, two string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM core.users WHERE telegram_id=101`).Scan(&one))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT id FROM core.users WHERE telegram_id=102`).Scan(&two))
	omitted := strings.Replace(
		strings.Replace(syntheticMessage, "message-one", "a-private", 1),
		"ordinary synthetic text",
		"PRIVATE_OMISSION_CANARY",
		1,
	)
	retained := strings.Replace(
		strings.Replace(syntheticMessage, "message-one", "b-retained", 1),
		`"user_id":101`,
		`"user_id":102`,
		1,
	)
	retained = strings.TrimSuffix(retained, "}") + `,"tokens":99999}`
	excluded := strings.Replace(
		strings.Replace(syntheticMessage, "message-one", "c-excluded", 1),
		"ordinary synthetic text",
		"EXCLUDED_CANARY",
		1,
	)
	excluded = strings.Replace(excluded, `"user_id":101`, `"user_id":103`, 1)
	stage, plan, resolutions, rows, r := messageInputs(t, omitted, retained, excluded)
	r.Messages[0].Owner = one
	r.Messages[0].Disposition = "omitted"
	r.Messages[0].Provenance = "private"
	r.Messages[1].Owner = two
	r.Messages[1].ArchiveFields = rows.Messages[1].Candidate.ArchiveFields
	r.Messages[2].Owner = ""
	r.Messages[2].Disposition = "excluded"
	r.Messages[2].Provenance = "owner_excluded"
	writeMessageResolutions(t, resolutions, r)
	summary, err := migrate.ApplyMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, 1, summary.Omitted)
	require.Equal(t, 1, summary.Excluded)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts`).Scan(&count))
	require.Zero(t, count)
	var dumped string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT string_agg(row_to_json(e)::text,'') FROM core.conversation_events e`).
			Scan(&dumped),
	)
	require.NotContains(t, dumped, "PRIVATE_OMISSION_CANARY")
	require.NotContains(t, dumped, "EXCLUDED_CANARY")
	_, err = db.Exec(
		t.Context(),
		`DROP TABLE migrate_import.message_receipts; WITH removed AS (DELETE FROM core.legacy_message_references WHERE event_id=(SELECT min(id) FROM core.conversation_events) RETURNING event_id) DELETE FROM core.conversation_events WHERE id IN(SELECT event_id FROM removed)`,
	)
	require.NoError(t, err)
	_, err = migrate.ReconcileMessages(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "message_reference_missing")
}
