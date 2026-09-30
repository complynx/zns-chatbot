package legacyfood

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// OwnerCapabilities identifies the current food event without disclosing its
// catalogue to a restricted actor. Commands still authorize inside their transaction.
type OwnerCapabilities struct {
	EventID   string `json:"event_id"`
	CanReview bool   `json:"can_review"`
	CanExport bool   `json:"can_export"`
}

func (s Service) OwnerCapabilities(ctx context.Context, actor string) (OwnerCapabilities, error) {
	var result OwnerCapabilities
	err := s.DB.QueryRow(ctx, `SELECT COALESCE((SELECT f.event_id
 FROM core.food_events f JOIN core.pass_events p ON p.id=f.event_id
 WHERE f.bot_id=$1 AND p.finishes_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM core.users WHERE id=$2 AND can_book)
 ORDER BY p.display_order,p.finishes_at,p.id LIMIT 1),'')`, s.BotID, actor).Scan(&result.EventID)
	if err != nil || result.EventID == "" {
		return result, core.DatabaseOperationError(err)
	}
	err = s.DB.QueryRow(ctx, `SELECT COALESCE(bool_or(can_review),false), COALESCE(bool_or(can_export),false)
 FROM core.food_admins WHERE event_id=$1 AND owner=$2`, result.EventID, actor).Scan(&result.CanReview, &result.CanExport)
	return result, core.DatabaseOperationError(err)
}

// EventCapabilities is scoped to an explicit previously bound event. It does
// not select a different event when a continuation resumes.
func (s Service) EventCapabilities(ctx context.Context, actor, event string) (OwnerCapabilities, error) {
	result := OwnerCapabilities{}
	if _, err := s.Event(ctx, actor, event); err != nil {
		return result, err
	}
	result.EventID = event
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(bool_or(can_review),false), COALESCE(bool_or(can_export),false)
 FROM core.food_admins WHERE event_id=$1 AND owner=$2`, event, actor).Scan(&result.CanReview, &result.CanExport)
	return result, core.DatabaseOperationError(err)
}
