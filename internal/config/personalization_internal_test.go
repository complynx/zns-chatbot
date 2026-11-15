package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalizationIdentityRequiresSeparateCompleteMachine(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		machine PersonalizationIdentity
		valid   bool
	}{
		{"disabled", PersonalizationIdentity{}, true},
		{"partial", PersonalizationIdentity{Subject: "billing"}, false},
		{"delegation actor", PersonalizationIdentity{Subject: "actor", ClientID: "client", ClientSecret: "secret"}, false},
		{"separate machine", PersonalizationIdentity{Subject: "billing", ClientID: "client", ClientSecret: "secret"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := Config{Identity: Identity{ActorID: "actor"}, Loom: Loom{Personalization: test.machine}}
			if test.valid {
				require.NoError(t, c.validatePersonalization())
			} else {
				require.Error(t, c.validatePersonalization())
			}
		})
	}
}
