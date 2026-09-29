package identity

const memoryProvenanceAudience = "zns-memory-provenance"

// MemoryProvenanceToken binds a trusted host request to one authenticated owner.
// It cannot be used as a user token or a notification delivery token.
func (s Signer) MemoryProvenanceToken(owner string) string {
	return s.token(owner, memoryProvenanceAudience)
}

func (s Signer) VerifyMemoryProvenance(token string) (string, error) {
	return s.verify(token, memoryProvenanceAudience)
}
