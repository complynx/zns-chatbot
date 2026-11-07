package registrationdetails_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestEventRequirementsReportOnlyMissingFieldIdentifiers(t *testing.T) {
	t.Parallel()
	rules := registrationdetails.Requirements{PassportNumber: true}
	require.Equal(
		t,
		[]registrationdetails.Field{registrationdetails.LegalName, registrationdetails.PassportNumber},
		rules.Missing(registrationdetails.Status{}),
	)
	require.Equal(
		t,
		[]registrationdetails.Field{registrationdetails.PassportNumber},
		rules.Missing(registrationdetails.Status{LegalNamePresent: true}),
	)
	require.Empty(t, rules.Missing(registrationdetails.Status{LegalNamePresent: true, PassportNumberPresent: true}))
	require.Empty(
		t,
		(registrationdetails.Requirements{LegalName: true}).Missing(registrationdetails.Status{LegalNamePresent: true}),
	)
	for _, raw := range []string{`{"legal_name":null}`, `{"passport_number":"yes"}`, `{"unknown":true}`} {
		var decoded registrationdetails.Requirements
		require.Error(t, json.Unmarshal([]byte(raw), &decoded))
	}
}
