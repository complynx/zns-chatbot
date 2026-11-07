package db

import (
	"fmt"
	"reflect"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func describeAssignmentQuote(quote AssignmentQuote) string {
	text := fmt.Sprintf("price %s %s", minorAmount(quote.PriceMinor, quote.CurrencyScale), quote.Currency)
	if quote.Tier != nil {
		text += fmt.Sprintf(", tier %d (%s)", quote.Tier.Number, quote.Tier.ID)
	}
	if quote.Deposit != nil {
		text += fmt.Sprintf(
			", deposit minimum %s %s",
			minorAmount(quote.Deposit.RequiredMinor, quote.CurrencyScale),
			quote.Currency,
		)
	}
	return text
}

// AssignmentQuote freezes the price terms shown for one recipient's approval.
type AssignmentQuote struct {
	RegistrationID uuid.UUID                      `json:"registration_id"`
	PriceMinor     int64                          `json:"price_minor"`
	Currency       string                         `json:"currency"`
	CurrencyScale  int32                          `json:"currency_scale"`
	Tier           *registrations.TierSnapshot    `json:"tier,omitempty"`
	Deposit        *registrations.DepositSnapshot `json:"deposit,omitempty"`
}

func assignmentQuotes(
	event events.Event,
	prices []registrations.PriceQuote,
	recipients ...uuid.UUID,
) ([]AssignmentQuote, error) {
	if len(prices) != len(recipients) {
		return nil, registrations.ErrInvalid
	}
	quotes := make([]AssignmentQuote, len(prices))
	for i := 0; i < len(prices) && i < len(recipients); i++ {
		price := prices[i]
		quotes[i] = AssignmentQuote{
			RegistrationID: recipients[i],
			PriceMinor:     price.PriceMinor,
			Currency:       event.Document.Registration.Currency,
			CurrencyScale:  event.Document.Registration.CurrencyScale,
			Tier:           price.Tier,
		}
		if event.Document.Deposit != nil {
			deposit, err := event.Document.Deposit.Snapshot(price.PriceMinor, event.Document.Registration.ClosesAt)
			if err != nil {
				return nil, err
			}
			quotes[i].Deposit = &deposit
		}
	}
	return quotes, nil
}

func checkAssignmentQuotes(event events.Event, approved, current []AssignmentQuote) error {
	if len(approved) == 0 && event.Document.Registration.Pricing == nil {
		return nil // Old flat-price preparations remain usable under their event version.
	}
	if !reflect.DeepEqual(approved, current) {
		return ErrConflict
	}
	return nil
}
