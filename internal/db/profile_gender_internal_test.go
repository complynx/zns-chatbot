package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenderChangeRetainsOnlyConfirmedOrStillRelevantPronouns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, gender, pronouns string
	}{
		{"changed_guess", GenderSourceInferred, "female", ""},
		{"unchanged_guess", GenderSourceInferred, "male", "he/him"},
		{"explicit_pronouns_are_independent", GenderSourceExplicit, "female", "he/him"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			profile := UserProfile{Gender: "male", Pronouns: "he/him", GenderSource: tc.source, PronounsSource: tc.source}
			changed, err := applyGenderMutation(&profile, ProfileMutation{Gender: &tc.gender, GenderSource: tc.source})
			require.NoError(t, err)
			require.Equal(t, tc.gender != "male", changed)
			require.Equal(t, tc.pronouns, profile.Pronouns)
			require.Equal(t, tc.source, profile.GenderSource)
		})
	}
}
