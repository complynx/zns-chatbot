package bot

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// Admission is persisted before transport. A missing completion is uncertain,
// never evidence that Telegram did not receive the file.
func (b *Bot) deliverModernOrder(
	ctx context.Context,
	owner, name string,
	record agenthost.ScriptToolRecord,
) (any, error) {
	request := record.ModernOrder
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner || source.in.chat != request.Chat {
		return nil, botdelivery.ErrBinding
	}
	if record.Source == nil || !record.Source.Valid() {
		return nil, appclient.ErrReadStale
	}
	if err := b.checkOrderDeliverySource(ctx, owner, record.Source); err != nil {
		return nil, err
	}
	family := botFamilyModernOrderExport
	kind := "modern_order_delivery:" + name
	ref := botdelivery.Reference{Family: family, Event: request.Event, Update: request.Update, Source: record.Source}
	if name != modernOrdersExport {
		if record.Order == nil {
			return nil, errors.New("observed receipt missing")
		}
		ref.Family = botFamilyModernOrderProof
		ref.Object, ref.Version, ref.ProofAttempt = record.Order.OrderID, record.Order.Version, record.Order.Attempt
		kind += ":" + record.Order.OrderID
	}
	ref.Continuation = botdelivery.Continuation{Kind: botDocumentKind, Key: kind}
	if receipt, found, err := b.observeModernOrderDocument(ctx, owner, request.Chat, ref); err != nil || found {
		return receipt, err
	}
	// Older admitted sends remain terminal/uncertain. Never infer a retry from a missing Telegram ID.
	var previous botdelivery.ModernReceipt
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, request.Update, kind).
		Scan(&previous)
	if err == nil {
		return previous, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	observed, err := b.queueBotDocument(ctx, owner, request.Chat, ref)
	if err != nil {
		return nil, err
	}
	receipt := botdelivery.ModernReceipt{
		Status:    string(observed.State),
		EventID:   request.Event,
		ChatID:    request.Chat,
		MessageID: observed.MessageID,
	}
	if documentDelivered(observed) {
		intent, readErr := botdelivery.Read(ctx, b.DB, b.Delivery.BotID, observed.Reference, false)
		if readErr != nil {
			return nil, readErr
		}
		receipt.Status = botReceiptDelivered
		if intent.Receipt.Document != nil {
			doc := intent.Receipt.Document
			receipt.Filename, receipt.SHA256, receipt.Bytes = doc.Filename, doc.SHA256, doc.Bytes
		}
	}
	return receipt, nil
}

// A repeated tool call may inherit more evidence from earlier results. Observe
// the original effect without changing its admitted source or scheduling a send.
func (b *Bot) observeModernOrderDocument(
	ctx context.Context, owner string, chat int64, ref botdelivery.Reference,
) (botdelivery.ModernReceipt, bool, error) {
	operation, effect := botdelivery.ResultOperation(
		owner,
		ref.Update,
		"document:"+ref.Family+":"+ref.Event+":"+ref.Object,
	)
	intent, err := botdelivery.Read(ctx, b.DB, b.Delivery.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return botdelivery.ModernReceipt{}, false, nil
	}
	if err != nil {
		return botdelivery.ModernReceipt{}, false, err
	}
	ref.Kind, ref.Generation = botdelivery.DocumentIntent, ref.Source.Generation
	original := intent.Reference
	if original.Source == nil || !original.Source.Valid() {
		return botdelivery.ModernReceipt{}, true, botdelivery.ErrBinding
	}
	original.Source = ref.Source
	if intent.Owner != owner || intent.Chat != chat || !reflect.DeepEqual(original, ref) {
		return botdelivery.ModernReceipt{}, true, botdelivery.ErrBinding
	}
	if err = b.checkOrderDeliverySource(ctx, owner, intent.Reference.Source); err != nil {
		return botdelivery.ModernReceipt{}, true, err
	}
	receipt := botdelivery.ModernReceipt{
		Status:    string(intent.State),
		EventID:   ref.Event,
		ChatID:    chat,
		MessageID: intent.MessageID,
	}
	if documentDelivered(intent.Observation()) {
		receipt.Status = botReceiptDelivered
	}
	if doc := intent.Receipt.Document; doc != nil {
		receipt.Filename, receipt.SHA256, receipt.Bytes = doc.Filename, doc.SHA256, doc.Bytes
	}
	return receipt, true, nil
}

func orderDeliveryDenied(err error) bool {
	if errors.Is(err, appclient.ErrReadStale) || errors.Is(err, appclient.ErrProvisioningDenied) {
		return true
	}
	problem, ok := errors.AsType[*core.ProblemError](err)
	if !ok {
		return false
	}
	switch problem.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
		return true
	default:
		return false
	}
}
