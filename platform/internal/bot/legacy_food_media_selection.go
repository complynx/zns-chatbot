package bot

import (
	"context"
	"slices"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Only the last displayed owner-bound choices supply command coordinates.
// Model-selected IDs and prices cannot authorize an undisplayed food target.
func resolvedFoodReceipt(in incoming, plan agent.Plan, input agent.Input) *agent.FoodReceiptTarget {
	p := plan.MediaAction
	if p == nil || p.Intent != mediaReceipt || p.OrderID == "" || p.RegistrationEvent != "" ||
		input.MediaContext == nil {
		return nil
	}
	evidence, _ := receiptFollowupEvidence(in, input, p.MediaID)
	evidence = strings.ReplaceAll(evidence, p.OrderID, "")
	// Exactly one kind must be named, even when the other kind is unavailable.
	if foodKindNamed(legacyfood.Meals, evidence) == foodKindNamed(legacyfood.Activity, evidence) {
		return nil
	}
	var selected *agent.FoodReceiptTarget
	for _, hint := range input.MediaContext.Pending {
		if hint.ID != p.MediaID {
			continue
		}
		for _, choice := range hint.Choices {
			target := choice.FoodTarget
			if target == nil || target.EventID == "" ||
				choice.Action != "food_"+target.Kind ||
				!foodKindNamed(target.Kind, evidence) {
				continue
			}
			if selected != nil {
				return nil
			}
			copyTarget := *target
			selected = &copyTarget
		}
	}
	if selected != nil && (selected.OrderID != p.OrderID || (p.FoodKind != "" && p.FoodKind != selected.Kind)) {
		return nil
	}
	return selected
}

func foodKindNamed(kind, evidence string) bool {
	words := []string{"meals", "meal", "питание", "питания", "еду", "еда", "еды"}
	if kind == legacyfood.Activity {
		words = []string{"activities", "activity", "активности", "активностей"}
	} else if kind != legacyfood.Meals {
		return false
	}
	for _, field := range strings.FieldsFunc(strings.ToLower(evidence), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if slices.Contains(words, field) {
			return true
		}
	}
	return false
}

func (b *Bot) chooseAgentFoodReceipt(
	ctx context.Context,
	in incoming,
	id string,
	target agent.FoodReceiptTarget,
	source readsource.Derivation,
) error {
	return b.selectFoodReceipt(ctx, in, foodButtonCommand{MediaID: id, Command: legacyfood.Command{
		EventID: target.EventID, OrderID: target.OrderID, Version: target.Version,
		Kind: target.Kind, Generation: target.Generation, Name: foodSubmitProof, Key: id,
	}}, originAgent, &source)
}
