//nolint:testpackage // Private normalization defines the legacy request hash contract.
package db

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProfileLatinOmissionPreservesLegacyRequestBytes(t *testing.T) {
	t.Parallel()
	m, err := normalizeProfileMutation(
		ProfileMutation{OperationID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			Kind: "set_name", FirstName: "Анна", LastName: "Иванова"},
	)
	require.NoError(t, err)
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	//nolint:testifylint // Exact bytes, including field order, define legacy idempotency hashes.
	require.Equal(
		t,
		"{\"operation_id\":\"00000000-0000-0000-0000-000000000001\",\"expected_version\":0,"+
			"\"kind\":\"set_name\",\"full_name\":\"Анна Иванова\","+
			"\"first_name\":\"Анна\",\"last_name\":\"Иванова\","+
			"\"expectation_id\":\"00000000-0000-0000-0000-000000000000\"}",
		string(raw),
	)
	var old UserProfile
	require.NoError(t, json.Unmarshal([]byte(`{"version":1,"full_name":"Анна Иванова"}`), &old))
	raw, err = json.Marshal(old)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "_latin")
}

func TestProfileLatinValidationAndExplicitClear(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"Анна", "Ivanova"}, {"Anna", ""}, {"Anna\nX", "Ivanova"},
		{"Anna\u05b0", "Ivanova"}, {"Anna\u093e", "Ivanova"},
		{strings.Repeat("A", 201), "B"}, {strings.Repeat("A", 128), strings.Repeat("B", 128)}, {"123", "Ivanova"}} {
		m := ProfileMutation{OperationID: uuid.New(), Kind: "set_name", FirstName: "Анна", LastName: "Иванова",
			FirstNameLatin: &pair[0], LastNameLatin: &pair[1]}
		_, err := normalizeProfileMutation(m)
		require.ErrorIs(t, err, ErrInvalidProfile, "%q", pair)
	}
	first, last := "  Anne-Marie ", "D’A\u0301vila"
	m := ProfileMutation{
		OperationID:    uuid.New(),
		Kind:           "set_name",
		FirstName:      "Анна",
		LastName:       "Иванова",
		FirstNameLatin: &first,
	}
	_, err := normalizeProfileMutation(m)
	require.ErrorIs(t, err, ErrInvalidProfile)
	m.LastNameLatin = &last
	normalized, err := normalizeProfileMutation(m)
	require.NoError(t, err)
	require.Equal(t, "Anne-Marie", *normalized.FirstNameLatin)
	first, last = "", ""
	_, err = normalizeProfileMutation(m)
	require.NoError(t, err)
	m.Kind, m.BotLanguage = "set_language", "en"
	_, err = normalizeProfileMutation(m)
	require.ErrorIs(t, err, ErrInvalidProfile)
}
