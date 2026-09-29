package bot

import (
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func savedPlanSource(plan interaction.SavedPlan) (readsource.Derivation, error) {
	if plan.PassAuthority == nil {
		return readsource.Derivation{}, errors.New("missing original plan source")
	}
	source := readsource.Derivation{
		PrivateHistory: plan.PassAuthority.PrivateHistory,
		Generation:     &plan.HistoryGeneration,
		Authorities:    plan.PassAuthority.ReadAuthorities,
	}.Clone()
	if !source.Valid() {
		return readsource.Derivation{}, errors.New("invalid original plan source")
	}
	return source, nil
}
