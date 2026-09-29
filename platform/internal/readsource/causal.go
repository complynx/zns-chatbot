package readsource

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

func CloneAuthorities(refs []Authority) []Authority {
	if refs == nil {
		return nil
	}
	result := append([]Authority{}, refs...)
	for i, a := range result {
		if a.Causal != nil {
			source := *a.Causal
			if source.Generation != nil {
				generation := *source.Generation
				source.Generation = &generation
			}
			if source.Authorities != nil {
				source.Authorities = append([]Authority{}, source.Authorities...)
			}
			result[i].Causal = &source
		}
	}
	return result
}

func sourceLeaves(refs []Authority) []Authority {
	result := []Authority{}
	for _, a := range refs {
		if a.Causal != nil {
			result = append(result, a.Causal.Authorities...)
		} else {
			result = append(result, a)
		}
	}
	return result
}

// Lock validates flat origin evidence and the current reader independently.
// All source actors/events are locked before any domain grant or history fence.
func Lock(ctx context.Context, tx pgx.Tx, actor string, refs []Authority) ([]bool, error) {
	validity, err := LockValidity(ctx, tx, actor, refs)
	return validity.Reader, err
}

// Validity separates permanent origin loss from a particular reader's denial.
type Validity struct {
	Origin []bool
	Reader []bool
}

func LockValidity(ctx context.Context, tx pgx.Tx, actor string, refs []Authority) (Validity, error) {
	return lockProposalValidity(ctx, tx, actor, refs)
}

func lockFlatValidity(ctx context.Context, tx pgx.Tx, actor string, refs []Authority) (Validity, error) {
	for _, a := range refs {
		if !Valid([]Authority{a}) {
			return Validity{}, &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_history"}
		}
	}
	if err := lockEvents(ctx, tx, refs); err != nil {
		return Validity{}, err
	}
	if err := lockActors(ctx, tx, []string{actor}, refs, false); err != nil {
		return Validity{}, err
	}
	leaves := sourceLeaves(refs)
	if err := lockSharedGate(ctx, tx, leaves); err != nil {
		return Validity{}, err
	}
	result := Validity{Origin: make([]bool, len(refs)), Reader: make([]bool, len(refs))}
	for i, a := range refs {
		var err error
		if a.Causal == nil {
			var valid []bool
			valid, err = lockLeaves(ctx, tx, actor, []Authority{a})
			if err == nil {
				result.Origin[i], result.Reader[i] = valid[0], valid[0]
			}
		} else {
			result.Origin[i], result.Reader[i], err = lockCausalDomains(ctx, tx, actor, *a.Causal)
		}
		if err != nil {
			return Validity{}, err
		}
	}
	if err := lockCausalHistory(ctx, tx, actor, refs, result); err != nil {
		return Validity{}, err
	}
	return result, nil
}

// History fences follow domain grants and use a stable owner order.
func lockCausalHistory(ctx context.Context, tx pgx.Tx, actor string, refs []Authority, result Validity) error {
	actors := append(causalActors(refs), actor)
	if err := fence.LockOwners(ctx, tx, actors); err != nil {
		return err
	}
	slices.Sort(actors)
	for _, origin := range slices.Compact(actors) {
		for i, a := range refs {
			if a.Causal == nil || a.Causal.Actor != origin {
				continue
			}
			err := fence.LockGeneration(ctx, tx, origin, a.Causal.Generation)
			var problem *core.ProblemError
			if errors.As(err, &problem) && problem.Code == "history_stale" {
				result.Origin[i], result.Reader[i] = false, false
				continue
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func lockSharedGate(ctx context.Context, tx pgx.Tx, refs []Authority) error {
	for _, a := range refs {
		if a.Knowledge.Kind == knowledgeauthority.SharedMemory {
			return knowledgeauthority.LockSharedGate(ctx, tx)
		}
	}
	return nil
}

func lockCausalDomains(ctx context.Context, tx pgx.Tx, reader string, source CausalSource) (bool, bool, error) {
	valid, err := lockLeaves(ctx, tx, source.Actor, source.Authorities)
	if err != nil || slices.Contains(valid, false) {
		return false, false, err
	}
	if source.Published || reader == source.Actor {
		return true, true, nil
	}
	if source.PrivateHistory {
		return true, false, nil
	}
	for _, a := range source.Authorities {
		if a.Knowledge.Kind == knowledgeauthority.PrivateMemory {
			return true, false, nil
		}
	}
	valid, err = lockLeaves(ctx, tx, reader, source.Authorities)
	return true, !slices.Contains(valid, false), err
}

func causalActors(refs []Authority) []string {
	actors := []string{}
	for _, a := range refs {
		if a.Causal != nil {
			actors = append(actors, a.Causal.Actor)
		}
	}
	return actors
}
