package migrate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityPolicyRequiresCompleteExplicitDecisions(t *testing.T) {
	t.Parallel()
	for _, users := range []string{
		`[{"legacy_key":"one","can_book":null}]`,
		`[{"legacy_key":"one"}]`,
		`[{"legacy_key":"one","can_book":false},{"legacy_key":"one","can_book":true}]`,
		`[{"legacy_key":"one","can_book":false,"subject":"invented"}]`,
	} {
		_, err := decodeIdentityPolicy(
			[]byte(`{"version":1,"plan_sha256":"hash","reviewed":true,"users":`+users+`}`),
			"hash",
		)
		require.Error(t, err)
	}
	policy, err := decodeIdentityPolicy(
		[]byte(`{"version":1,"plan_sha256":"hash","reviewed":true,"users":[{"legacy_key":"one","can_book":false}]}`),
		"hash",
	)
	require.NoError(t, err)
	assert.False(t, policy["one"])
}

func TestIdentityCandidatesPreflightAllRows(t *testing.T) {
	t.Parallel()
	first := "First"
	long := strings.Repeat("я", 201)
	row := UserPlanRecord{Kind: "user", Status: userCandidate, Legacy: UserLegacyReference{Key: "one"},
		Candidate: &UserCandidate{TelegramID: 101, FirstName: &first},
		Blockers:  []string{"target_identity_unresolved", "eligibility_policy_unresolved"}}
	valid, err := json.Marshal(row)
	require.NoError(t, err)
	_, err = identityCandidates(valid, map[string]bool{})
	require.EqualError(t, err, "identity_policy_missing")
	_, err = identityCandidates(valid, map[string]bool{"one": false, "extra": true})
	require.EqualError(t, err, "identity_policy_extra")
	row.Candidate.FirstName = &long
	row.Legacy.Key = "two"
	invalid, err := json.Marshal(row)
	require.NoError(t, err)
	_, err = identityCandidates(append(append(valid, '\n'), invalid...), map[string]bool{"one": false, "two": true})
	require.EqualError(t, err, "identity_metadata_invalid")
}
