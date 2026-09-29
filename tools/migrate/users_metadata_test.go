package migrate_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsersMetadataBoundariesAndPrivateReplay(t *testing.T) {
	t.Parallel()
	record := map[string]any{"_id": "metadata", "bot_id": 77, "user_id": 101,
		"username": strings.Repeat("A_", 32), "first_name": strings.Repeat("😀", 256),
		"last_name": "PRIVATE-LAST", "print_name": strings.Repeat("Ж", 513)}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	stage := usersStage(t, string(data))
	target := filepath.Join(t.TempDir(), "users.jsonl")
	summary, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.False(t, summary.ApplyReady)
	row := userRows(t, target)[0]
	require.NotNil(t, row.Candidate)
	assert.Equal(t, record["username"], *row.Candidate.Username)
	assert.Equal(t, record["first_name"], *row.Candidate.FirstName)
	assert.Equal(t, record["last_name"], *row.Candidate.LastName)
	assert.Equal(t, record["print_name"], *row.Candidate.PrintName)
	assert.Equal(t, *row.Candidate.PrintName, *row.Candidate.DisplayName)
	assert.EqualValues(t, -1, row.Candidate.TelegramMetadataUpdate)
	assert.Empty(t, *row.Candidate.LegalName)
	assert.ElementsMatch(t, []string{"target_identity_unresolved", "eligibility_policy_unresolved"}, row.Blockers)
	data, err = json.Marshal(summary)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "PRIVATE-LAST")
	replay, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, replay.Reused)
}

func TestUsersMetadataInvalidAndAbsentDispositions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, fields, field, disposition string }{
		{"absent", ``, "first_name", "default_absent"},
		{"nullable", `,"username":null,"last_name":null`, "username", "default_null"},
		{"first null", `,"first_name":null`, "first_name", "invalid_value"},
		{"print null", `,"print_name":null`, "print_name", "invalid_value"},
		{"username chars", `,"username":"@abc"`, "username", "invalid_value"},
		{"wrong type", `,"last_name":false`, "last_name", "invalid_value"},
		{"control", `,"first_name":"a\nb"`, "first_name", "invalid_value"},
		{"noncharacter", `,"last_name":"\uffff"`, "last_name", "invalid_value"},
		{"surrogate", `,"print_name":"\ud800"`, "print_name", "invalid_value"},
		{"long username", `,"username":"` + strings.Repeat("a", 65) + `"`, "username", "invalid_value"},
		{"long first", `,"first_name":"` + strings.Repeat("界", 257) + `"`, "first_name", "invalid_value"},
		{"long print", `,"print_name":"` + strings.Repeat("a", 514) + `"`, "print_name", "invalid_value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stage := usersStage(t, `{"_id":"u","bot_id":77,"user_id":101`+test.fields+`}`)
			target := filepath.Join(t.TempDir(), "users.jsonl")
			_, err := migrate.PlanUsers(stage, target, migrate.DefaultLimits())
			require.NoError(t, err)
			row := userRows(t, target)[0]
			assert.Contains(
				t,
				row.Fields,
				migrate.UserFieldDisposition{Field: test.field, Disposition: test.disposition},
			)
			assert.Contains(t, row.Blockers, "display_name_unresolved")
			if test.disposition == "invalid_value" {
				assert.Contains(t, row.Blockers, "invalid_mapped_field")
			}
		})
	}
}
