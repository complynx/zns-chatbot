package migrate_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usersStage(t *testing.T, records ...string) string {
	t.Helper()
	directory, manifest := snapshot(t)
	manifest.Files = nil
	addFile(
		t,
		directory,
		&manifest,
		"users.jsonl",
		"users",
		"records",
		[]byte(strings.Join(records, "\n")+"\n"),
		int64(len(records)),
	)
	writeManifest(t, directory, manifest)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(directory, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	return stage
}

func userRows(t *testing.T, path string) []migrate.UserPlanRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	rows := []migrate.UserPlanRecord{}
	for scanner.Scan() {
		var row migrate.UserPlanRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &row))
		if row.Kind == "user" {
			rows = append(rows, row)
		}
	}
	require.NoError(t, scanner.Err())
	return rows
}

func TestUsersPlanScopeIdentityPresenceAndPrivateValues(t *testing.T) {
	t.Parallel()
	stage := usersStage(
		t,
		`{"_id":{"$oid":"0123456789abcdef01234567"},"bot_id":77,"user_id":{"$numberLong":"101"},"print_name":"Display Only","legal_name":"Synthetic Legal Name","passport_number":"PRIVATE-PASSPORT","role":"leader","legal_name_frozen":false,"banned":false,"language_code":"by-BY","massage_specialist":{"notify_bookings":false,"notify_next":true},"unknown_active":{"flag":true}}`,
		`{"_id":"other-bot","bot_id":88,"user_id":101,"legal_name":"Other User"}`,
		`{"_id":"unscoped","user_id":303,"role":"follower"}`,
	)
	target := filepath.Join(t.TempDir(), "users.jsonl")
	summary, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.False(t, summary.ApplyReady)
	assert.EqualValues(t, 1, summary.Candidates)
	assert.EqualValues(t, 1, summary.Excluded)
	assert.EqualValues(t, 1, summary.Invalid)
	encoded, err := json.Marshal(summary)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "PRIVATE-PASSPORT")
	assert.NotContains(t, string(encoded), "Synthetic Legal Name")
	rows := userRows(t, target)
	require.Len(t, rows, 3)
	own := rows[0]
	require.NotNil(t, own.Candidate)
	assert.EqualValues(t, 101, own.Candidate.TelegramID)
	assert.Nil(t, own.Candidate.TargetOwner)
	assert.Nil(t, own.Candidate.ZitadelSubject)
	assert.Nil(t, own.Candidate.ZitadelIssuer)
	assert.Nil(t, own.Candidate.CanBook)
	assert.True(t, own.Candidate.Frozen, "Python freezes by field presence even when false")
	assert.True(t, own.Candidate.LegacyBanned, "Python bans by field presence even when false")
	assert.Equal(t, "Synthetic Legal Name", *own.Candidate.LegalName)
	assert.Equal(t, "PRIVATE-PASSPORT", *own.Candidate.Passport)
	assert.Equal(t, "Display Only", *own.Candidate.DisplayName)
	assert.Equal(t, "by-BY", *own.Candidate.Language)
	assert.Equal(t, "ru", *own.Candidate.PresentationLocale)
	require.NotNil(t, own.Candidate.Notifications)
	assert.False(t, *own.Candidate.Notifications.NotifyBookings)
	assert.True(t, *own.Candidate.Notifications.NotifyNext)
	assert.Contains(t, own.Blockers, "target_identity_unresolved")
	assert.Contains(t, own.Blockers, "ban_policy_unmapped")
	assert.Contains(t, own.Blockers, "unmapped_fields")
	assert.Contains(t, own.Blockers, "specialist_event_mapping_unresolved")
	assert.Nil(t, rows[1].Candidate)
	assert.Nil(t, rows[2].Candidate)
	assert.NotEmpty(t, own.Legacy.Key)
	assert.JSONEq(t, `{"$oid":"0123456789abcdef01234567"}`, string(own.Legacy.RecordID))
}

func TestUsersPlanNeverInfersLegalIdentityOrGrants(t *testing.T) {
	t.Parallel()
	stage := usersStage(
		t,
		`{"_id":1,"bot_id":77,"user_id":101,"print_name":"Display Person","role":"admin","admin":true,"can_book":true,"language_code":"zh-Hant","state":{"state":"input","passport":"private"},"massage_specialist":{}}`,
	)
	target := filepath.Join(t.TempDir(), "users.jsonl")
	_, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	rows := userRows(t, target)
	require.Len(t, rows, 1)
	candidate := rows[0].Candidate
	require.NotNil(t, candidate)
	assert.Empty(t, *candidate.LegalName)
	assert.Empty(t, *candidate.Passport)
	assert.False(t, candidate.Frozen)
	assert.Nil(t, candidate.Role)
	assert.Nil(t, candidate.CanBook)
	assert.Equal(t, "zh-Hant", *candidate.Language)
	assert.Nil(t, candidate.PresentationLocale)
	assert.Contains(t, rows[0].Blockers, "profile_role_invalid")
	assert.Contains(t, rows[0].Blockers, "active_state_unmapped")
	assert.Contains(t, rows[0].Blockers, "locale_mapping_required")
	assert.True(t, *candidate.Notifications.NotifyBookings)
	assert.True(t, *candidate.Notifications.NotifyNext)
}

func TestUsersPlanDuplicateTelegramIDNeverPublishes(t *testing.T) {
	t.Parallel()
	stage := usersStage(t, `{"_id":1,"bot_id":77,"user_id":101}`, `{"_id":2,"bot_id":77,"user_id":101}`)
	target := filepath.Join(t.TempDir(), "users.jsonl")
	_, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.EqualError(t, err, "duplicate_telegram_identity")
	_, err = os.Stat(target)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUsersPlanExactReplayAndImmutableInput(t *testing.T) {
	t.Parallel()
	stage := usersStage(
		t,
		`{"_id":1,"bot_id":77,"user_id":101,"print_name":"Synthetic","role":"follower","language_code":"pl-PL","state":{"state":""}}`,
	)
	target := filepath.Join(t.TempDir(), "users.jsonl")
	first, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	second, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, second.Reused)
	assert.Equal(t, first.ArtifactSHA256, second.ArtifactSHA256)
	rows := userRows(t, target)
	assert.Equal(t, "en", *rows[0].Candidate.PresentationLocale)
	assert.NotContains(t, rows[0].Blockers, "active_state_unmapped")
	require.NoError(t, os.WriteFile(target, []byte("do not overwrite"), 0600))
	_, err = migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.EqualError(t, err, "plan_replay_mismatch")
	current, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "do not overwrite", string(current))
	require.NoError(t, os.WriteFile(filepath.Join(stage, "snapshot", "users.jsonl"), []byte("tampered"), 0600))
	_, err = migrate.PlanUsers(stage, filepath.Join(t.TempDir(), "new.jsonl"), migrate.DefaultLimits())
	require.Error(t, err)
}

func TestUsersPlanInvalidMappedValuesAreBlockers(t *testing.T) {
	t.Parallel()
	stage := usersStage(
		t,
		`{"_id":1,"bot_id":77,"user_id":101,"legal_name":false,"passport_number":null,"language_code":null,"legal_name_frozen":null,"massage_specialist":{"notify_next":0}}`,
	)
	target := filepath.Join(t.TempDir(), "users.jsonl")
	_, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	candidate := userRows(t, target)[0]
	assert.Nil(t, candidate.Candidate.LegalName)
	require.NotNil(t, candidate.Candidate.Passport)
	assert.Empty(
		t,
		*candidate.Candidate.Passport,
		"null source retains presence while runtime stores an empty passport",
	)
	assert.True(t, candidate.Candidate.Frozen)
	assert.Equal(t, "en", *candidate.Candidate.PresentationLocale)
	assert.Nil(t, candidate.Candidate.Notifications.NotifyNext)
	assert.Contains(t, candidate.Blockers, "invalid_mapped_field")
	assert.Contains(t, candidate.Blockers, "notification_value_invalid")
}

func TestUsersLegacyReferenceSurvivesNewSnapshotContent(t *testing.T) {
	t.Parallel()
	stages := []string{
		usersStage(t, `{"_id":{"$numberInt":"7"},"bot_id":77,"user_id":101,"print_name":"Before"}`),
		usersStage(t, `{"_id":7,"bot_id":77,"user_id":101,"print_name":"After"}`),
	}
	keys := []string{}
	hashes := []string{}
	for _, stage := range stages {
		target := filepath.Join(t.TempDir(), "users.jsonl")
		_, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
		require.NoError(t, err)
		row := userRows(t, target)[0]
		keys = append(keys, row.Legacy.Key)
		hashes = append(hashes, row.Legacy.RecordSHA256)
	}
	assert.Equal(t, keys[0], keys[1])
	assert.NotEqual(t, hashes[0], hashes[1])
}
