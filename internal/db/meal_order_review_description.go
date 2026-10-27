package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/orders"
)

// Presentation renders the frozen composition for human consent. It performs no
// catalog lookup and never substitutes current names, prices or service times.
func (m MealTerms) Presentation(language string) (string, error) {
	zone, err := time.LoadLocation(m.Timezone)
	if err != nil {
		return "", err
	}
	var body strings.Builder
	writeMealOrderReviewDetails(&body, &m, zone, language == "ru")
	return body.String(), nil
}

// Render only frozen names/times/charges. Combo components are descriptive;
// only the original charge lines carry prices.
func writeMealOrderReviewDetails(body *strings.Builder, meal *MealTerms, zone *time.Location, ru bool) {
	if meal == nil {
		return
	}
	heading := "Meal composition"
	if ru {
		heading = "Состав питания"
	}
	fmt.Fprintf(body, "\n%s:\n", heading)
	for _, day := range meal.Days {
		fmt.Fprintf(body, "%s\n", strconv.Quote(mealReviewLabel(day.Name, ru)))
		writeMealServiceReview(body, meal, day.ID, day.Lunch, "lunch", zone, ru)
		writeMealServiceReview(body, meal, day.ID, day.Dinner, "dinner", zone, ru)
	}
}

func mealReviewLabel(label orders.Label, ru bool) string {
	if ru {
		return label.RU
	}
	return label.EN
}

func writeMealServiceReview(body *strings.Builder, meal *MealTerms, day string,
	service *MealServiceTerms, kind string, zone *time.Location, ru bool,
) {
	if service == nil {
		return
	}
	title := "Lunch"
	if kind == "dinner" {
		title = "Dinner"
	}
	if ru {
		title = "Обед"
		if kind == "dinner" {
			title = "Ужин"
		}
	}
	fmt.Fprintf(body, "%s: %s — %s (%s)\n", title,
		service.StartsAt.In(zone).Format(time.RFC3339), service.EndsAt.In(zone).Format(time.RFC3339), zone.String())
	// Individual charge lines already contain their names. Keep a separate
	// composition list only for unpriced combo components, avoiding duplicate
	// labels that can overflow a valid localized meal's immutable review receipt.
	priced := make(map[string]bool)
	for _, charge := range meal.Lines {
		if charge.DayID == day && charge.Meal == kind && charge.ItemID != "" {
			priced[charge.ItemID] = true
		}
	}
	for _, item := range service.Items {
		if priced[item.ID] {
			continue
		}
		fmt.Fprintf(body, "  %s\n", strconv.Quote(mealReviewLabel(item.Name, ru)))
	}
	for _, charge := range meal.Lines {
		if charge.DayID != day || charge.Meal != kind {
			continue
		}
		fmt.Fprintf(
			body,
			"  %s: %s\n",
			strconv.Quote(mealReviewChargeLabel(service, charge, ru)),
			paymentReviewMoney(
				charge.AmountMinor,
				int32(meal.Quote.CurrencyScale),
				meal.Quote.Currency,
			),
		) // #nosec G115 -- frozen meal scale is validated0..6.
	}
}

func mealReviewChargeLabel(service *MealServiceTerms, line orders.MealPriceLine, ru bool) string {
	for _, item := range service.Items {
		if item.ID == line.ItemID {
			return mealReviewLabel(item.Name, ru)
		}
	}
	switch line.Mode {
	case orders.LunchComboWithSoup:
		if ru {
			return "Комплекс с супом"
		}
		return "Combo with soup"
	case orders.LunchComboWithoutSoup:
		if ru {
			return "Комплекс без супа"
		}
		return "Combo without soup"
	case orders.LunchNone, orders.LunchIndividual:
		return line.ItemID
	default:
		return line.ItemID
	}
}
