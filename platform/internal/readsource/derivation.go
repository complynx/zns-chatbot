package readsource

// Derivation binds host-observed source evidence to a model-derived operation.
// It is not a grant. Zero is a valid generation; missing fields are not valid.
type Derivation struct {
	PrivateHistory bool        `json:"private_history"`
	Generation     *int64      `json:"generation"`
	Authorities    []Authority `json:"authorities"`
}

func (d Derivation) Valid() bool {
	return d.Generation != nil && *d.Generation >= 0 && d.Authorities != nil && Valid(d.Authorities)
}

// Clone detaches evidence from mutable worker and plan buffers.
func (d Derivation) Clone() Derivation {
	result := d
	if d.Generation != nil {
		generation := *d.Generation
		result.Generation = &generation
	}
	if d.Authorities != nil {
		result.Authorities = CloneAuthorities(d.Authorities)
	}
	return result
}
