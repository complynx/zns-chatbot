package sandbox

import (
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func validateAttestedOwners(owners map[int64]string) error {
	if owners == nil {
		return nil
	}
	if len(owners) == 0 {
		return errors.New("attested sandbox owners must not be empty")
	}
	for sender, owner := range owners {
		_, supported := identity.Subject(sender)
		if !supported || owner == "" || strings.TrimSpace(owner) != owner {
			return errors.New("attested sandbox owners require supported fixture actors and nonblank owners")
		}
	}
	return nil
}

func (f *Fake) domainOwner(sender int64) (string, bool) {
	if f.attestedOwners == nil {
		return identity.Subject(sender)
	}
	owner, known := f.attestedOwners[sender]
	return owner, known
}

func attestedModelOwner(owners map[int64]string, owner string) bool {
	if owners == nil {
		return syntheticFixtureOwner(owner)
	}
	if validateAttestedOwners(owners) != nil {
		return false
	}
	for _, attested := range owners {
		if owner == attested {
			return true
		}
	}
	return false
}
