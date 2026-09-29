package bot

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

func (b *Bot) renderFood(ctx context.Context, in incoming, event, id string, review bool) error {
	view, err := b.API.foodView(ctx, in.owner, event, id, review)
	if err != nil {
		return b.foodFailure(ctx, in, err)
	}
	pref, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	order := view.Order
	mealStatus, err := i18n.Translate(pref.Language, i18n.ID("food.status."+order.MealPayment.Status), nil)
	if err != nil {
		return err
	}
	activityStatus, err := i18n.Translate(pref.Language, i18n.ID("food.status."+order.ActivityPayment.Status), nil)
	if err != nil {
		return err
	}
	mealEntry := foodMealEntry(view, time.Now())
	card := i18n.FoodCard
	if !review && !mealEntry {
		card = i18n.FoodCardClosed
	}
	text, err := i18n.Translate(
		pref.Language,
		card,
		map[string]string{
			"order":          order.ID,
			legacyfood.Meals: foodAmount(order.MealTotal),
			"activities":     foodAmount(order.ActivityTotal),
			"mealstatus":     mealStatus,
			"activitystatus": activityStatus,
		},
	)
	if err != nil {
		return err
	}
	instructions, err := foodInstructions(pref.Language, view.Instructions)
	if err != nil {
		return err
	}
	text += instructions
	markup, err := b.foodMarkup(ctx, in.owner, pref.Language, order, review, mealEntry)
	if err != nil {
		return err
	}
	return b.deliverOrderCard(
		ctx,
		in.owner,
		"food:"+order.EventID+":"+order.ID,
		telegram.Send{ChatID: in.chat, Text: text, Markup: markup},
	)
}

const foodMinorUnits = 100

func foodAmount(amount legacyfood.Amount) string {
	return fmt.Sprintf("%d.%02d", amount/foodMinorUnits, amount%foodMinorUnits)
}

func foodInstructions(language string, instructions map[string]string) (string, error) {
	for _, locale := range i18n.FallbackLocales(language) {
		if value := instructions[string(locale)]; value != "" {
			text, err := i18n.Translate(language, i18n.FoodInstructions, map[string]string{"instructions": value})
			return "\n" + text, err
		}
	}
	return "", nil
}

type foodAction struct {
	label                i18n.ID
	name, kind, activity string
	generation           int64
}

func foodOwnerActions(order legacyfood.Order) []foodAction {
	actions := []foodAction{}
	if !order.MealPayment.Locked() && order.Complete && order.MealTotal > 0 {
		actions = append(
			actions,
			foodAction{i18n.FoodPayMeals, foodBeginPayment, legacyfood.Meals, "", order.MealPayment.Generation},
		)
	}
	if !order.MealPayment.Locked() && len(order.Meals) > 0 {
		actions = append(actions, foodAction{i18n.FoodDelete, "delete_meals", "", "", 0})
	}
	if order.ActivityPayment.Locked() {
		return actions
	}
	for _, entry := range []struct {
		label    i18n.ID
		activity string
	}{{i18n.FoodOpen, "open"}, {i18n.FoodYoga, "yoga"}, {i18n.FoodCacao, "cacao"}, {i18n.FoodSound, "soundhealing"}, {i18n.FoodAll, "all"}, {i18n.FoodClasses, "classes"}} {
		actions = append(actions, foodAction{entry.label, "toggle_activity", "", entry.activity, 0})
	}
	if order.ActivityTotal > 0 {
		actions = append(
			actions,
			foodAction{
				i18n.FoodPayActivities,
				foodBeginPayment,
				legacyfood.Activity,
				"",
				order.ActivityPayment.Generation,
			},
		)
	}
	return actions
}

func foodReviewActions(order legacyfood.Order) []foodAction {
	actions := []foodAction{}
	for _, payment := range []legacyfood.Payment{order.MealPayment, order.ActivityPayment} {
		if payment.Status != legacyfood.Submitted {
			continue
		}
		actions = append(
			actions,
			foodAction{i18n.RegistrationPaymentFile, foodShowProof, payment.Kind, "", payment.Generation},
			foodAction{
				i18n.FoodAccept,
				passAccept,
				payment.Kind,
				"",
				payment.Generation,
			},
			foodAction{i18n.FoodReject, knowledgeRejectDecision, payment.Kind, "", payment.Generation},
		)
	}
	return actions
}

func (b *Bot) foodMarkup(
	ctx context.Context,
	owner, language string,
	order legacyfood.Order,
	review bool,
	mealEntry bool,
) (telegram.Markup, error) {
	markup := telegram.Markup{Rows: [][]telegram.Button{}}
	actions := foodOwnerActions(order)
	if !mealEntry {
		actions = slices.DeleteFunc(actions, func(action foodAction) bool {
			return action.kind == legacyfood.Meals || action.name == scriptFoodDeleteMeals
		})
	}
	if review {
		actions = foodReviewActions(order)
	}
	for _, action := range actions {
		text, err := foodActionLabel(language, order, action, review)
		if err != nil {
			return markup, err
		}
		command := legacyfood.Command{
			EventID:    order.EventID,
			OrderID:    order.ID,
			Version:    order.Version,
			Name:       action.name,
			Kind:       action.kind,
			Activity:   action.activity,
			Generation: action.generation,
		}
		button, err := b.foodButton(ctx, owner, text, foodButtonCommand{Command: command})
		if err != nil {
			return markup, err
		}
		markup.Rows = append(markup.Rows, []telegram.Button{button})
	}
	if !review && mealEntry && b.WebAppURL != "" {
		button, err := b.foodWebButton(language, order)
		if err != nil {
			return markup, err
		}
		markup.Rows = append(markup.Rows, []telegram.Button{button})
	}
	return markup, nil
}

func foodActionLabel(language string, order legacyfood.Order, action foodAction, review bool) (string, error) {
	text, err := i18n.Translate(language, action.label, nil)
	if err != nil {
		return "", err
	}
	if order.Activities[action.activity] {
		text = "✓ " + text
	}
	if !review || action.kind == "" {
		return text, nil
	}
	label := i18n.FoodMeals
	if action.kind == legacyfood.Activity {
		label = i18n.FoodActivities
	}
	kind, err := i18n.Translate(language, label, nil)
	return text + " · " + kind, err
}

func (b *Bot) foodWebButton(language string, order legacyfood.Order) (telegram.Button, error) {
	address, err := webappurl.Route(b.WebAppURL, "/menu")
	if err != nil {
		return telegram.Button{}, err
	}

	address.RawQuery = url.Values{
		"pass_key": {order.EventID},
		"order_id": {order.ID},
		"lang":     {string(i18n.NormalizeLocale(language))},
	}.Encode()
	text, err := i18n.Translate(language, i18n.OrderMealsProfile, nil)
	return telegram.Button{Text: text, WebApp: &telegram.WebApp{URL: address.String()}}, err
}

// The normal meal entry follows the legacy deadline visibility rule. Direct
// menu saves and independent activity actions retain their domain semantics.
func foodMealEntry(view legacyfood.View, now time.Time) bool {
	return !now.After(view.Event.Deadline) ||
		(view.Order.MealPayment.Locked() && view.Order.MealTotal > 0 && len(view.Order.Meals) > 0)
}
