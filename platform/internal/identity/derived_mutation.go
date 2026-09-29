package identity

const derivedMutationAudience = "zns-derived-mutation"

// DerivedMutationToken authenticates host evidence for one owner. The receiving
// route must also verify the owner's separate live user credential.
func (s Signer) DerivedMutationToken(owner string) string {
	return s.token(owner, derivedMutationAudience)
}

func (s Signer) VerifyDerivedMutation(token string) (string, error) {
	return s.verify(token, derivedMutationAudience)
}
