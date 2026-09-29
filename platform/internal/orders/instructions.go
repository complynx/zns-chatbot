package orders

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

type PaymentContact struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Region     string `json:"region"`
	TelegramID int64  `json:"telegram_id"`
}

// PaymentInstructions is a read-only, owner-bound view of current payment options.
type PaymentInstructions struct {
	OrderID      string           `json:"order_id"`
	Version      int64            `json:"version"`
	State        string           `json:"state"`
	TotalBYN     Money            `json:"total_byn"`
	TotalRUB     string           `json:"total_rub"`
	CanPay       bool             `json:"can_pay"`
	Transfer     string           `json:"transfer"`
	Language     string           `json:"language"`
	PaymentAdmin string           `json:"payment_admin"`
	Contacts     []PaymentContact `json:"contacts"`
}

func (s Service) Instructions(ctx context.Context, actor, event, id string) (PaymentInstructions, error) {
	var result PaymentInstructions
	var localized map[string]string
	var legacy string
	// One statement keeps totals, state, deadline, contacts and authorization consistent.
	err := s.DB.QueryRow(ctx, `SELECT o.id,o.version,o.state,o.choice->'total',
		o.state IN ('unpaid','cash') AND e.deadline>statement_timestamp() AND (o.choice->>'total')::numeric>0,
		e.transfer_instructions,o.payment_admin,
		COALESCE((SELECT jsonb_agg(jsonb_build_object('id',u.id,'name',u.name,'region',a.region,
		'telegram_id',u.telegram_id) ORDER BY u.id) FROM core.order_admins a JOIN core.users u ON u.id=a.owner
		WHERE a.event_id=e.id AND a.country='be' AND u.can_book),'[]'::jsonb),
		COALESCE(NULLIF(actor.language,''),'en'),e.transfer_instructions_localized
		FROM core.orders o JOIN core.order_events e ON e.id=o.event_id JOIN core.users actor ON actor.id=o.owner
		WHERE o.owner=$1 AND o.event_id=$2 AND o.id=$3 AND o.state<>'deleted' AND actor.can_book`,
		actor, event, id).Scan(&result.OrderID, &result.Version, &result.State, &result.TotalBYN,
		&result.CanPay, &legacy, &result.PaymentAdmin, &result.Contacts, &result.Language, &localized)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusNotFound, "payment_context_unavailable")
	}
	if err != nil {
		return result, err
	}
	if localized == nil {
		localized = map[string]string{}
	}
	if localized["ru"] == "" {
		localized["ru"] = legacy
	}
	for _, locale := range i18n.FallbackLocales(result.Language) {
		if text := localized[string(locale)]; text != "" {
			result.Transfer = text
			break
		}
	}
	const maxInstructionRunes = 1000
	if utf8.RuneCountInString(result.Transfer) > maxInstructionRunes {
		return result, problem(http.StatusInternalServerError, "invalid_payment_configuration")
	}
	const rubPerBYN = 30
	value := int64(result.TotalBYN) * rubPerBYN
	result.TotalRUB = fmt.Sprintf("%d.%02d", value/centsPerUnit, value%centsPerUnit)
	if !result.CanPay {
		result.Transfer = ""
	}
	return result, nil
}
