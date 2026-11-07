package agentskills

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedSkillsContract(t *testing.T) {
	t.Parallel()
	namePattern := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	for _, item := range bindings() {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			skill := read(item.name)
			require.Regexp(t, namePattern, skill.Name)
			require.LessOrEqual(t, len(skill.Name), 64)
			require.NotEmpty(t, skill.Usecase)
			require.LessOrEqual(t, utf8.RuneCountInString(skill.Usecase), 1024)
			require.NotEmpty(t, skill.Body)
			require.LessOrEqual(t, utf8.RuneCountInString(skill.Body), 16000)
			require.LessOrEqual(t, len(skill.Require), 50)
			seen := map[string]bool{}
			for _, dependency := range skill.Require {
				require.Contains(t, dependency, ".")
				require.False(t, seen[dependency], "duplicate dependency %s", dependency)
				seen[dependency] = true
			}
			for line := range strings.SplitSeq(skill.Body, "\n") {
				require.LessOrEqual(t, utf8.RuneCountInString(line), 120)
			}
		})
	}
}

func TestSkillPublicationStaysWithAuthorizedPublisher(t *testing.T) {
	t.Parallel()
	for _, item := range bindings() {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			method := map[string]any{"id": item.method, "effect": "read"}
			Annotate(item.module, []map[string]any{method})
			definitions, ok := method["skills"].([]Definition)
			require.True(t, ok)
			require.Len(t, definitions, 1)
			require.Equal(t, item.name, definitions[0].Name)
			raw, err := json.Marshal(definitions)
			require.NoError(t, err)
			require.NotContains(t, string(raw), `"source":`)
			require.NotContains(t, string(raw), `"version":`)
			other := map[string]any{"id": "unrelated"}
			Annotate(item.module, []map[string]any{other})
			require.NotContains(t, other, "skills")
		})
	}
}

func TestHiddenNameActionsHaveExactDiscoveryPath(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"request_name", "set_name", "complete_name", "cancel_name"} {
		method := map[string]any{"id": name}
		Annotate("zinusya_profile", []map[string]any{method})
		require.Equal(t, "hidden", method["visibility"])
		require.Contains(t, read("profile-name").Body, name)
	}
	for _, name := range []string{"set_language", "operation"} {
		method := map[string]any{"id": name}
		Annotate("zinusya_profile", []map[string]any{method})
		require.Equal(t, "list", method["visibility"])
	}
}
