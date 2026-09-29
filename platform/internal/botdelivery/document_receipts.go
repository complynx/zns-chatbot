package botdelivery

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func (s Service) projectDocumentReceipt(ctx context.Context, tx pgx.Tx, i Intent) error {
	if i.State != delivery.Succeeded || i.MessageID <= 0 || i.Receipt.Kind != "document" || i.Receipt.Document == nil {
		return ErrBinding
	}
	doc := i.Receipt.Document
	var content any = map[string]any{"message_id": i.MessageID, "filename": doc.Filename, "event_id": i.Reference.Event}
	if i.Reference.Family == "modern_order_export" || i.Reference.Family == "modern_order_proof" {
		content = ModernReceipt{
			Status:    "delivered",
			EventID:   i.Reference.Event,
			ChatID:    i.Chat,
			Filename:  doc.Filename,
			SHA256:    doc.SHA256,
			Bytes:     doc.Bytes,
			MessageID: i.MessageID,
		}
	}
	if i.Receipt.Key != "" {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) ON CONFLICT(owner,update_id,kind) DO NOTHING`,
			i.Owner,
			i.Reference.Update,
			i.Receipt.Key,
			content,
		); err != nil {
			return err
		}
	}
	if i.Reference.Notice == "" {
		return nil
	}
	if i.Reference.Family == "food_orders_export" || i.Reference.Family == "food_summary_export" {
		var complete bool
		if err := tx.QueryRow(ctx, `SELECT count(*)=2 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind IN ('food_orders_export','food_summary_export')`, i.Owner, i.Reference.Update).
			Scan(&complete); err != nil ||
			!complete {
			return err
		}
	}
	return s.enqueueResultTx(
		ctx,
		tx,
		ResultRequest{
			Owner:     i.Owner,
			Chat:      i.Chat,
			Update:    i.Reference.Update,
			Effect:    "document-notice:" + string(i.Reference.Notice),
			Reference: Reference{Family: "static", Generation: i.Reference.Generation, Source: i.Reference.Source},
			Result:    StoredResult{Notice: i.Reference.Notice, Source: i.Reference.Source},
		},
	)
}
