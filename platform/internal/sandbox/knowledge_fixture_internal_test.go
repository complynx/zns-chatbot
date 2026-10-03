package sandbox

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKnowledgeFixtureFiniteBinding(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"", "sandbox-festival", "sandbox-past"} {
		for _, permission := range []string{"review", "curate"} {
			for _, action := range []string{"read", "grant", "revoke"} {
				require.NoError(t, (KnowledgeFixture{Stand: RegistrationFixtureStand, Action: action,
					Scope: scope, Permission: permission}).Validate())
			}
		}
	}
	valid := KnowledgeFixture{Stand: RegistrationFixtureStand, Action: "read", Permission: "review"}
	for _, mutate := range []func(*KnowledgeFixture){
		func(f *KnowledgeFixture) { f.Stand = "production" },
		func(f *KnowledgeFixture) { f.Action = "init" },
		func(f *KnowledgeFixture) { f.Scope = RegistrationFixtureEventA },
		func(f *KnowledgeFixture) { f.Permission = "admin" },
	} {
		f := valid
		mutate(&f)
		require.Error(t, f.Validate())
	}
}

func TestKnowledgeFixtureReadbackContainsOnlyCapabilities(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(KnowledgeFixtureState{})
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.Len(t, fields, 5)
	var scope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fields["scope"], &scope))
	require.Len(t, scope, 4)
	require.Contains(t, scope, "can_review")
	require.Contains(t, scope, "can_curate")
}
