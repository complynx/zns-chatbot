package task

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestProfileContextIncludesBoundaryToneWithAndWithoutSkills(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"ru", "en", "pt-BR"} {
		for _, skillsEnabled := range []bool{false, true} {
			name := language + "/legacy"
			if skillsEnabled {
				name = language + "/skills"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var text strings.Builder
				text.WriteString("User request.")
				require.NoError(t, appendProfileContext(
					&text, db.UserProfile{Version: 1, FirstName: "Alex", LastName: "Example"},
					telegram.User{LanguageCode: language}, language, skillsEnabled,
				))
				prompt := text.String()
				require.Equal(t, 1, strings.Count(prompt, personaBoundaryInstructions))
				require.Contains(t, prompt, personaPrompt(language)+personaBoundaryInstructions)
				if skillsEnabled {
					require.Contains(t, prompt, personaBoundaryInstructions+skillsInstructions)
				}
			})
		}
	}
}
