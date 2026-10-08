package orders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// ChoiceSnapshot contains current fingerprints and booking permission, not a
// permit. Every page must obtain a new snapshot before exposing a retained draft.
type ChoiceSnapshot struct {
	Catalog string `json:"catalog"`
	Order   string `json:"order,omitempty"`
	CanBook bool   `json:"can_book"`
}

// ChoiceSnapshot uses the same catalog and owner-scoped order reads as a quote.
// It avoids transferring their private bodies; it does not avoid loading them.
func (s Service) ChoiceSnapshot(ctx context.Context, actor, event, id string) (ChoiceSnapshot, error) {
	value, err := s.Event(ctx, event)
	if err != nil {
		return ChoiceSnapshot{}, err
	}
	result := ChoiceSnapshot{Catalog: CatalogSnapshot(value)}
	if err = core.DatabaseOperationError(s.DB.QueryRow(ctx,
		`SELECT can_book FROM core.users WHERE id=$1`, actor).Scan(&result.CanBook)); err != nil {
		return ChoiceSnapshot{}, err
	}
	if id == "" {
		return result, nil
	}
	order, err := s.Get(ctx, actor, event, id)
	if err != nil {
		return ChoiceSnapshot{}, err
	}
	result.Order, err = OrderSnapshot(order)
	if err != nil {
		return ChoiceSnapshot{}, err
	}
	return result, nil
}

// OrderSnapshot identifies the complete authorized order, including its version.
func OrderSnapshot(order Order) (string, error) {
	data, err := json.Marshal(order)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
