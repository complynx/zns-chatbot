package registrations_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestOptionalEventRequirementsPreserveExistingRuleDocuments(t *testing.T) {
	t.Parallel()
	original := registrations.Rules{
		LeaderCapacity:       1,
		FollowerCapacity:     1,
		Currency:             "BYN",
		CurrencyScale:        2,
		ClosesAt:             time.Now().UTC().Truncate(time.Microsecond),
		PaymentWindowSeconds: 60,
	}
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "details_requirements")
	var decoded registrations.Rules
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Nil(t, decoded.DetailsRequirements)
	reproduced, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.Equal(t, raw, reproduced)
	original.DetailsRequirements = &registrationdetails.Requirements{PassportNumber: true}
	normalized, err := original.Normalize()
	require.NoError(t, err)
	require.True(t, normalized.DetailsRequirements.LegalName)
	require.False(t, original.DetailsRequirements.LegalName, "Normalization must not mutate caller-owned settings")
}
