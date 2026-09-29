package readsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

var ErrLimit = errors.New("source_authority_limit")

// Merge never drops evidence to fit a budget.
func Merge(groups ...[]Authority) ([]Authority, error) {
	result := []Authority{}
	for _, group := range groups {
		if !Valid(group) {
			return nil, ErrLimit
		}
		for _, ref := range group {
			a := CloneAuthorities([]Authority{ref})[0]
			if !slices.ContainsFunc(result, func(b Authority) bool { return Equal(a, b) }) {
				result = append(result, a)
			}
		}
	}
	if !Valid(result) {
		return nil, ErrLimit
	}
	slices.SortFunc(result, func(a, b Authority) int {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return bytes.Compare(x, y)
	})
	return result, nil
}

// Capture flattens inherited groups and records the current host source once.
func Capture(actor string, d Derivation) ([]Authority, error) {
	if !d.Valid() {
		return nil, errors.New("invalid derivation")
	}
	leaves := []Authority{}
	inherited := []Authority{}
	for _, a := range d.Authorities {
		if a.Causal == nil {
			leaves = append(leaves, a)
		} else {
			inherited = append(inherited, a)
		}
	}
	generation := *d.Generation
	own := Authority{
		Causal: &CausalSource{
			Actor:          actor,
			Generation:     &generation,
			PrivateHistory: d.PrivateHistory,
			Authorities:    leaves,
		},
	}
	// Reuse an identical origin generation by unioning its direct evidence.
	for i, a := range inherited {
		if !a.Causal.Published && a.Causal.Actor == actor && *a.Causal.Generation == generation &&
			a.Causal.PrivateHistory == d.PrivateHistory {
			merged, err := Merge(a.Causal.Authorities, leaves)
			if err != nil {
				return nil, err
			}
			copied := *a.Causal
			copied.Authorities = merged
			inherited[i] = Authority{Causal: &copied}
			return Merge(inherited)
		}
	}
	return Merge(inherited, []Authority{own})
}
