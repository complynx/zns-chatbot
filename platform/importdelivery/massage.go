// Package importdelivery exposes the shared queue's transaction boundary to
// offline importers. It does not create owner intents or authorize recipients.
package importdelivery

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// MassageNotice identifies an already inserted, owner-bound additional notice.
type MassageNotice struct {
	ID, Chat int64
}

// RegisterMassage registers imported intents after all owner writes. The caller
// owns the transaction and must roll back on any error. Acquire no owner locks
// after this final phase. No historical sent marker belongs in notices.
func RegisterMassage(ctx context.Context, tx pgx.Tx, botID int64, notices []MassageNotice) error {
	requests := make([]delivery.Registration, 0, len(notices))
	for _, notice := range notices {
		if notice.ID <= 0 || notice.Chat <= 0 {
			return errors.New("massage_import_delivery_invalid")
		}
		requests = append(requests, delivery.Registration{
			Reference: delivery.Reference{
				Owner: delivery.Massage, Key: strconv.FormatInt(notice.ID, 10), Effect: "send",
			},
			Destination: delivery.Destination{Chat: strconv.FormatInt(notice.Chat, 10)},
			Class:       delivery.Background,
		})
	}
	return delivery.RegisterBatch(ctx, tx, botID, requests)
}
