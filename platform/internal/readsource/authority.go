// Package readsource composes domain-owned evidence for derived text.
package readsource

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const MaxAuthorities = passbooking.MaxReadAuthorities

// Authority contains exactly one domain leaf. Neither leaf grants authority.
type Authority struct {
	Causal       *CausalSource                    `json:"causal,omitempty"`
	Registration passbooking.ReadAuthority        `json:"registration,omitzero"`
	Knowledge    knowledgeauthority.ReadAuthority `json:"knowledge,omitzero"`
	Food         legacyfood.ReadAuthority         `json:"food,omitzero"`
	Practitioner massage.ReadAuthority            `json:"practitioner,omitzero"`
}

// CausalSource is flat: Authorities contains domain leaves, never another group.
// Actor and PrivateHistory are supplied by the host, never by model arguments.
type CausalSource struct {
	Published      bool        `json:"published,omitempty"`
	Actor          string      `json:"actor"`
	Generation     *int64      `json:"generation"`
	PrivateHistory bool        `json:"private_history"`
	Authorities    []Authority `json:"authorities"`
}

const MaxAuthorityBytes = 1024 * 1024

func Valid(authorities []Authority) bool {
	count := len(authorities)
	if count > MaxAuthorities {
		return false
	}
	for _, a := range authorities {
		if a.Causal == nil {
			if !validLeaf(a) {
				return false
			}
			continue
		}
		source := a.Causal
		if a.Registration != (passbooking.ReadAuthority{}) ||
			a.Knowledge != (knowledgeauthority.ReadAuthority{}) ||
			a.Food != (legacyfood.ReadAuthority{}) ||
			a.Practitioner != (massage.ReadAuthority{}) ||
			source.Actor == "" ||
			len(source.Actor) > 200 ||
			strings.ContainsRune(source.Actor, 0) ||
			source.Generation == nil ||
			*source.Generation < 0 ||
			source.Authorities == nil {
			return false
		}
		count += len(source.Authorities)
		if count > MaxAuthorities {
			return false
		}
		for _, leaf := range source.Authorities {
			if !validLeaf(leaf) {
				return false
			}
		}
	}
	encoded, err := json.Marshal(authorities)
	return err == nil && len(encoded) <= MaxAuthorityBytes
}

func validLeaf(a Authority) bool {
	if a.Causal != nil {
		return false
	}
	count := 0
	valid := false
	if a.Registration != (passbooking.ReadAuthority{}) {
		count++
		valid = passbooking.ValidReadAuthorities([]passbooking.ReadAuthority{a.Registration})
	}
	if a.Knowledge != (knowledgeauthority.ReadAuthority{}) {
		count++
		valid = a.Knowledge.Valid()
	}
	if a.Food != (legacyfood.ReadAuthority{}) {
		count++
		valid = a.Food.Valid()
	}
	if a.Practitioner != (massage.ReadAuthority{}) {
		count++
		valid = a.Practitioner.Valid()
	}
	return count == 1 && valid
}

// Equal compares evidence structurally, including flat causal groups.
func Equal(a, b Authority) bool {
	if a.Registration != b.Registration || a.Knowledge != b.Knowledge || a.Food != b.Food ||
		a.Practitioner != b.Practitioner {
		return false
	}
	if a.Causal == nil || b.Causal == nil {
		return a.Causal == nil && b.Causal == nil
	}
	x, y := a.Causal, b.Causal
	if x.Published != y.Published || x.Actor != y.Actor || x.PrivateHistory != y.PrivateHistory ||
		x.Generation == nil ||
		y.Generation == nil ||
		*x.Generation != *y.Generation {
		return false
	}
	leafEqual := func(a, b Authority) bool {
		return a.Causal == nil && b.Causal == nil && a.Registration == b.Registration && a.Knowledge == b.Knowledge &&
			a.Food == b.Food &&
			a.Practitioner == b.Practitioner
	}
	for _, leaf := range x.Authorities {
		if !slices.ContainsFunc(y.Authorities, func(other Authority) bool { return leafEqual(leaf, other) }) {
			return false
		}
	}
	for _, leaf := range y.Authorities {
		if !slices.ContainsFunc(x.Authorities, func(other Authority) bool { return leafEqual(leaf, other) }) {
			return false
		}
	}
	return true
}

func Registration(authorities []passbooking.ReadAuthority) []Authority {
	result := make([]Authority, 0, len(authorities))
	for _, a := range authorities {
		result = append(result, Authority{Registration: a})
	}
	return result
}

// Lock follows registration event -> actor -> domain grant order. Domain checks
// run in the archive transaction, so a revocation cannot race the derived write.
func lockLeaves(ctx context.Context, tx pgx.Tx, actor string, authorities []Authority) ([]bool, error) {
	result := make([]bool, len(authorities))
	for i, a := range authorities {
		if !validLeaf(a) {
			return nil, &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_history"}
		}
		valid, err := lockLeaf(ctx, tx, actor, a)
		if err != nil {
			return nil, err
		}
		result[i] = valid
	}
	return result, nil
}

func lockLeaf(ctx context.Context, tx pgx.Tx, actor string, a Authority) (bool, error) {
	switch {
	case a.Registration != (passbooking.ReadAuthority{}):
		result, err := passbooking.LockReadAuthorities(ctx, tx, actor, []passbooking.ReadAuthority{a.Registration})
		if err != nil {
			return false, err
		}
		return result[0], nil
	case a.Knowledge != (knowledgeauthority.ReadAuthority{}):
		result, err := knowledgeauthority.LockReads(ctx, tx, actor, []knowledgeauthority.ReadAuthority{a.Knowledge})
		if err != nil {
			return false, err
		}
		return result[0], nil
	case a.Food != (legacyfood.ReadAuthority{}):
		return legacyfood.LockReadAuthority(ctx, tx, actor, a.Food)
	default:
		return massage.LockReadAuthority(ctx, tx, actor, a.Practitioner)
	}
}
