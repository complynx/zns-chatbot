package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

type modernDeliveryReceipt struct {
	Status    string `json:"status"`
	EventID   string `json:"event_id"`
	ChatID    int64  `json:"chat_id"`
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	Bytes     int    `json:"bytes"`
	MessageID int64  `json:"message_id,omitempty"`
}

// Admission is persisted before transport. A missing completion is uncertain,
// never evidence that Telegram did not receive the file.
func (b *Bot) deliverModernOrder(ctx context.Context, owner, name string, record scriptToolRecord) (any, error) {
	request := record.ModernOrder
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner || source.in.chat != request.Chat {
		return nil, errors.New("order delivery context changed")
	}
	var body []byte
	filename := "orders.xlsx"
	var err error
	if name == modernOrdersExport {
		body, err = b.API.ExportOrders(ctx, owner, request.Event)
	} else {
		if record.Order == nil {
			return nil, errors.New("observed receipt missing")
		}
		proof, readErr := b.API.DownloadOrderProof(ctx, owner, request.Event, record.Order.OrderID)
		if readErr != nil {
			return nil, readErr
		}
		if proof.Version != record.Order.Version || proof.Attempt != record.Order.Attempt {
			return nil, errScriptReadStale
		}
		body, filename = proof.Body, proof.Filename
	}
	if err != nil {
		return nil, err
	}
	kind := "modern_order_delivery:" + name
	if record.Order != nil {
		kind += ":" + record.Order.OrderID
	}
	digest := sha256.Sum256(body)
	receipt := modernDeliveryReceipt{
		Status:   "uncertain",
		EventID:  request.Event,
		ChatID:   request.Chat,
		Filename: filename,
		SHA256:   hex.EncodeToString(digest[:]),
		Bytes:    len(body),
	}
	tag, err := b.DB.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		owner,
		request.Update,
		kind,
		receipt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		var previous modernDeliveryReceipt
		err = b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, request.Update, kind).
			Scan(&previous)
		return previous, err
	}
	message, sendErr := b.TG.SendDocument(ctx, request.Chat, filename, body)
	if sendErr == nil {
		receipt.Status = "delivered"
		receipt.MessageID = message.ID
	}
	_, err = b.DB.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		request.Update,
		kind,
		receipt,
	)
	return receipt, err
}
