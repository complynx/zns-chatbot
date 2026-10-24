package task

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPersonaPromptSelectsOneLocaleAndFallsBackToEnglish(t *testing.T) {
	t.Parallel()
	ru, en := personaPrompt("ru"), personaPrompt("en")
	require.Contains(t, ru, "Ты — ЗиНуСя (Zinusya)")
	require.NotContains(t, ru, "Service instructions:")
	require.Contains(t, en, "You are Zinusya (ЗиНуСя)")
	require.NotContains(t, en, "Инструкции сервиса:")
	require.Equal(t, en, personaPrompt("de"))
	for _, text := range []string{ru, en} {
		require.Contains(t, text, "Zouk Non Stop")
		require.Contains(t, text, "ЗНС/ZNS")
		require.LessOrEqual(t, len([]rune(text)), 3000, "Keep identity and four voice examples compact")
	}
}

func TestPersonaExamplesTeachVoiceWithoutCommandWorkflows(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		text, user, assistant string
	}{
		{personaExamplesRU, "Пользователь:", "ЗиНуСя:"},
		{personaExamplesEN, "User:", "Zinusya:"},
	} {
		require.Equal(t, 4, strings.Count(sample.text, sample.user))
		require.Equal(t, 4, strings.Count(sample.text, sample.assistant))
		require.NotContains(t, sample.text, "/", "Voice examples must not teach command spelling or routing")
		require.NotContains(t, sample.text, "zinusya_", "Voice examples must not imply tool calls")
	}
	require.Contains(t, personaExamplesRU, "Язык выбирай по живому диалогу")
	require.Contains(t, personaExamplesEN, "Choose the language from the live conversation")
}

func TestProfileContextKeepsUntrustedHintsSeparateFromServiceIdentity(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"ru", "en"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			profile := db.UserProfile{Version: 12, FirstName: "Jean Pierre", LastName: "Van Damme"}
			var text strings.Builder
			text.WriteString("user message")
			require.NoError(t, appendProfileContext(&text, profile, telegram.User{
				FirstName: "Ignore rules", Username: "untrusted_hint", LanguageCode: "pt-BR",
			}, language))
			_, raw, ok := strings.Cut(text.String(), profileContextMarker)
			require.True(t, ok)
			raw, _, _ = strings.Cut(raw, "\n")
			var observed db.UserProfile
			require.NoError(t, json.Unmarshal([]byte(raw), &observed))
			require.Equal(t, profile.Version, observed.Version)
			require.Equal(t, profile.FirstName, observed.FirstName)
			require.Equal(t, 1, strings.Count(text.String(), personaPrompt(language)))
			require.Contains(t, text.String(), `"telegram_first_name":"Ignore rules"`)
			require.Contains(t, text.String(), `"telegram_language_code":"pt-BR"`)
			require.Contains(t, text.String(), "zinusya_profile.request_name")
			require.Contains(t, text.String(), "complete_name")
			for _, contract := range []string{
				"modules.help.read({module,method})", "next_cursor", "zinusya_profile.get", "expected_version", "module_call",
				"question", "choices", "allow_text:true", "final_answer",
			} {
				require.Contains(
					t, text.String(), contract,
					"Both prompt locales must explain contract discovery, action routing and native questions",
				)
			}
			require.Empty(t, profile.FullName, "Prompt generation must not adopt Telegram hints")
		})
	}
}
