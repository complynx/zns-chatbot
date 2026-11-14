package integration_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProfileGenderExplicitOverridesInferenceAndAppearsInBasicCardsAndPrompt(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	owner, token := registrationUser(t, h, 456, false)
	_, viewer := registrationUser(t, h, 29301, false)
	profile := setPeopleName(t, h, owner, "Alex", "Example")
	schemas := profileOutputSchemas(t, h, token)
	inferredOp := uuid.New()
	inferred := map[string]any{
		"expected_version": profile.Version, "gender": "male", "pronouns": "he/him", "source": "inferred",
	}
	code, raw := profileRequest(t, h, token, "set_gender", inferredOp, inferred)
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["set_gender"])
	require.Equal(t, "inferred", profile.GenderSource)
	code, raw = peopleRequest(t, h, viewer, "info", map[string]any{"user_id": owner})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"gender":"male"`)
	require.Contains(t, string(raw), `"gender_source":"inferred"`)
	require.Contains(t, string(raw), `"pronouns":"he/him"`)
	code, raw = peopleRequest(t, h, viewer, "search", map[string]any{"query": "Alex Example"})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "Alex Example")
	require.Contains(t, string(raw), `"gender_source":"inferred"`)
	require.Contains(t, string(raw), `"pronouns":"he/him"`)
	code, raw = profileRequest(t, h, token, "set_gender", uuid.New(), map[string]any{
		"expected_version": profile.Version, "gender": "nonbinary", "source": "explicit",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["set_gender"])
	require.Empty(t, profile.Pronouns, "A gender statement does not confirm guessed pronouns")
	code, raw = peopleRequest(t, h, viewer, "info", map[string]any{"user_id": owner})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"gender_source":"explicit"`)
	require.NotContains(t, string(raw), `"pronouns"`)
	require.NotContains(t, string(raw), "he/him")
	code, raw = profileRequest(t, h, token, "set_gender", uuid.New(), map[string]any{
		"expected_version": profile.Version,
		"gender":           "nonbinary",
		"pronouns":         "they/them; по-русски они",
		"source":           "explicit",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["set_gender"])
	require.Equal(t, "explicit", profile.GenderSource)
	require.Equal(t, "Alex Example", profile.FullName)
	code, raw = profileRequest(t, h, token, "set_gender", uuid.New(), map[string]any{
		"expected_version": profile.Version, "gender": "nonbinary", "source": "explicit",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["set_gender"])
	require.Equal(t, "they/them; по-русски они", profile.Pronouns, "Confirmed pronouns remain when omitted")
	profile = setPeopleName(t, h, owner, "Alex", "Updated")
	require.Equal(t, "nonbinary", profile.Gender)
	require.Equal(t, "explicit", profile.GenderSource)
	require.Equal(t, "they/them; по-русски они", profile.Pronouns)
	code, raw = profileRequest(t, h, token, "set_gender", uuid.New(), map[string]any{
		"expected_version": profile.Version, "gender": "male", "source": "inferred",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	unchanged, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, owner))
	require.NoError(t, err)
	require.Equal(t, profile.Version, unchanged.Version)
	require.Equal(t, "nonbinary", unchanged.Gender)
	code, raw = profileRequest(t, h, token, "set_gender", inferredOp, inferred)
	require.Equal(t, http.StatusOK, code, string(raw))
	original := validProfileReceipt(t, raw, schemas["set_gender"])
	require.Equal(
		t,
		"inferred",
		original.GenderSource,
		"Replay returns the original receipt without restoring old preferences",
	)
	code, raw = peopleRequest(t, h, viewer, "info", map[string]any{"user_id": owner})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"gender":"nonbinary"`)
	require.Contains(t, string(raw), `"gender_source":"explicit"`)
	require.NotContains(t, string(raw), "administration")
	task := h.submitText(t, 99281, "Расскажи о мероприятии")
	h.await(t, task, "completed")
	p.mu.Lock()
	prompt := p.createdInputs[len(p.createdInputs)-1]
	p.mu.Unlock()
	require.Contains(t, prompt, `"pronouns":"they/them; по-русски они"`)
	require.Contains(t, prompt, `"gender_source":"explicit"`)
	require.Contains(t, prompt, "zinusya_profile.set_gender")
	code, raw = profileRequest(t, h, token, "set_gender", uuid.New(), map[string]any{
		"expected_version": profile.Version, "gender": "", "pronouns": "", "source": "explicit",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	cleared := validProfileReceipt(t, raw, schemas["set_gender"])
	require.Empty(t, cleared.Gender)
	require.Empty(t, cleared.Pronouns)
	require.Empty(t, cleared.GenderSource)
	require.Empty(t, cleared.PronounsSource)
}

func TestProfileGenderAndPronounsHaveIndependentSources(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	owner, token := registrationUser(t, h, 456, false)
	_, viewer := registrationUser(t, h, 29301, false)
	profile := setPeopleName(t, h, owner, "Alex", "Example")
	schemas := profileOutputSchemas(t, h, token)
	write := func(arguments map[string]any) {
		t.Helper()
		arguments["expected_version"] = profile.Version
		code, raw := profileRequest(t, h, token, "set_gender", uuid.New(), arguments)
		require.Equal(t, http.StatusOK, code, string(raw))
		profile = validProfileReceipt(t, raw, schemas["set_gender"])
	}
	write(map[string]any{"gender": "male", "pronouns": "he/him", "source": "inferred"})
	write(map[string]any{"pronouns": "they/them", "source": "explicit"})
	require.Equal(t, "male", profile.Gender)
	require.Equal(t, "inferred", profile.GenderSource)
	require.Equal(t, "they/them", profile.Pronouns)
	require.Equal(t, "explicit", profile.PronounsSource)
	code, raw := peopleRequest(t, h, viewer, "info", map[string]any{"user_id": owner})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"gender_source":"inferred"`)
	require.Contains(t, string(raw), `"pronouns_source":"explicit"`)
	write(map[string]any{"gender": "female", "source": "inferred"})
	require.Equal(t, "female", profile.Gender)
	require.Equal(t, "they/them", profile.Pronouns)
	require.Equal(t, "explicit", profile.PronounsSource)
	version := profile.Version
	code, raw = profileRequest(t, h, token, "set_gender", uuid.New(), map[string]any{
		"expected_version": version, "pronouns": "she/her", "source": "inferred",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	profile, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, owner))
	require.NoError(t, err)
	require.Equal(t, version, profile.Version)
	require.Equal(t, "they/them", profile.Pronouns)
	write(map[string]any{"gender": "", "source": "explicit"})
	require.Empty(t, profile.Gender)
	require.Empty(t, profile.GenderSource)
	require.Equal(t, "they/them", profile.Pronouns)
	require.Equal(t, "explicit", profile.PronounsSource)
}
