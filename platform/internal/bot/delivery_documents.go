package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// queueBotDocument pins the private history before registering a transport effect.
// Only the delivery worker owns Telegram document transport.
func (b *Bot) queueBotDocument(
	ctx context.Context,
	owner string,
	chat int64,
	ref botdelivery.Reference,
) (botdelivery.Observation, error) {
	ref.Kind = botdelivery.DocumentIntent
	if ref.Update == 0 {
		origin, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
		if !ok || origin.owner != owner || origin.in.chat != chat {
			return botdelivery.Observation{}, botdelivery.ErrBinding
		}
		ref.Update = origin.update.ID
	}
	// Preserve existing known receipts across the migration. A missing or zero ID is not success.
	if ref.Continuation.Key != "" {
		var previous struct {
			MessageID int64 `json:"message_id"`
		}
		err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, ref.Update, ref.Continuation.Key).
			Scan(&previous)
		if err == nil && previous.MessageID > 0 {
			return botdelivery.Observation{State: delivery.Succeeded, MessageID: previous.MessageID}, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return botdelivery.Observation{}, err
		}
	}
	var generation int64
	if ref.Source != nil && ref.Source.Generation != nil {
		generation = *ref.Source.Generation
	} else {
		var err error
		generation, err = b.API.HistoryGeneration(ctx, owner)
		if err != nil {
			return botdelivery.Observation{}, err
		}
	}
	ref.Generation = &generation
	operation, effect := botdelivery.ResultOperation(
		owner,
		ref.Update,
		"document:"+ref.Family+":"+ref.Event+":"+ref.Object,
	)
	return b.enqueueBotIntent(ctx, owner, chat, operation, effect, ref, botDocumentKind)
}
func (b *Bot) renderBotDocument(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	r := i.Reference
	rendered := botRenderedDelivery{Receipt: r.Continuation}
	if r.Kind != botdelivery.DocumentIntent || r.Generation == nil {
		return rendered, botdelivery.ErrBinding
	}
	if err := b.API.CheckHistoryGeneration(ctx, i.Owner, *r.Generation); err != nil {
		return rendered, err
	}
	var err error
	switch r.Family {
	case botFamilyOrderExport, botFamilyModernOrderExport, botFamilyOrderProof, botFamilyModernOrderProof:
		rendered.Filename, rendered.Body, err = b.renderBotOrderDocument(ctx, i)
	case botFamilyPassExport, botFamilyPassProof:
		rendered.Filename, rendered.Body, rendered.ExportEvents, err = b.renderBotPassDocument(ctx, i)
	case botFamilyFoodOrdersExport,
		botFamilyFoodSummaryExport,
		botFamilyFoodProofMeals,
		botFamilyFoodProofActivity,
		botFamilyFoodReviewMeals,
		botFamilyFoodReviewActivity:
		rendered.Filename, rendered.Body, err = b.renderBotFoodDocument(ctx, i)
	case botFamilyAdminFile:
		rendered.Filename, rendered.Body, err = b.renderBotAdminDocument(ctx, i)
	default:
		return rendered, botdelivery.ErrBinding
	}
	if err != nil {
		return rendered, err
	}
	if err = b.API.CheckHistoryGeneration(ctx, i.Owner, *r.Generation); err != nil {
		return rendered, err
	}
	digest := sha256.Sum256(rendered.Body)
	rendered.Receipt.Document = &botdelivery.DocumentReceipt{
		Filename: rendered.Filename,
		SHA256:   hex.EncodeToString(digest[:]),
		Bytes:    len(rendered.Body),
	}
	return rendered, nil
}
func (b *Bot) renderBotOrderDocument(ctx context.Context, i botdelivery.Intent) (string, []byte, error) {
	r := i.Reference
	if r.Family == botFamilyOrderExport || r.Family == botFamilyModernOrderExport {
		body, err := b.API.ExportOrders(ctx, i.Owner, r.Event)
		if err == nil {
			err = b.checkOrderExportDelivery(ctx, i.Owner, r.Event, r.Source)
		}
		return "orders.xlsx", body, err
	}
	proof, err := b.API.DownloadOrderProof(ctx, i.Owner, r.Event, r.Object)
	if err != nil {
		return "", nil, err
	}
	if proof.Version != r.Version || proof.Attempt != r.ProofAttempt {
		return "", nil, botdelivery.ErrStale
	}
	err = b.checkOrderProofDelivery(ctx, i.Owner, r.Event, r.Object, proof, r.Source)
	return proof.Filename, proof.Body, err
}
func (b *Bot) renderBotPassDocument(ctx context.Context, i botdelivery.Intent) (string, []byte, []string, error) {
	r := i.Reference
	if r.Family == botFamilyPassExport {
		snapshot, err := b.Host.ExportPassSnapshot(ctx, i.Owner)
		if err != nil {
			return "", nil, nil, err
		}
		err = b.Host.CheckPassExportSnapshot(ctx, i.Owner, snapshot.Events)
		if err == nil {
			err = b.checkPassDeliverySource(ctx, i.Owner, r.Source)
		}
		return "passes.xlsx", snapshot.Body, snapshot.Events, err
	}
	proof, err := b.API.DownloadPassProof(ctx, i.Owner, r.Event, r.Object)
	if err != nil {
		return "", nil, nil, err
	}
	if proof.Version != r.Version || proof.Attempt != r.ProofAttempt {
		return "", nil, nil, botdelivery.ErrStale
	}
	return proof.Filename, proof.Body, nil, nil
}
func (b *Bot) renderBotFoodDocument(ctx context.Context, i botdelivery.Intent) (string, []byte, error) {
	r := i.Reference
	if r.Source != nil {
		if err := b.checkFoodDeliverySource(ctx, i.Owner, r.Source); err != nil {
			return "", nil, err
		}
	}
	var filename string
	var body []byte
	var err error
	if r.Family == botFamilyFoodOrdersExport || r.Family == botFamilyFoodSummaryExport {
		filename, body, err = b.renderBotFoodExport(ctx, i)
	} else {
		kind := legacyfood.Meals
		if r.Family == botFamilyFoodProofActivity || r.Family == botFamilyFoodReviewActivity {
			kind = legacyfood.Activity
		}
		command := legacyfood.Command{
			EventID:    r.Event,
			OrderID:    r.Object,
			Version:    r.Version,
			Generation: r.Attempt,
			Kind:       kind,
		}
		filename = "receipt"
		if r.Family == botFamilyFoodReviewMeals || r.Family == botFamilyFoodReviewActivity {
			body, err = b.API.FoodReviewProof(ctx, i.Owner, command)
		} else {
			body, err = b.API.FoodProof(ctx, i.Owner, command)
		}
	}
	if err == nil && r.Source != nil {
		err = b.checkFoodDeliverySource(ctx, i.Owner, r.Source)
	}
	return filename, body, err
}
func (b *Bot) renderBotAdminDocument(ctx context.Context, i botdelivery.Intent) (string, []byte, error) {
	var authorized struct {
		OK bool `json:"ok"`
	}
	if err := b.API.Call(ctx, i.Owner, http.MethodGet, "/v1/admin-utilities/authorize", nil, &authorized); err != nil {
		return "", nil, err
	}
	if !authorized.OK || !adminFileID.MatchString(i.Reference.Object) {
		return "", nil, botdelivery.ErrStale
	}
	var file telegram.File
	if err := b.TG.Call(ctx, "getFile", map[string]string{"file_id": i.Reference.Object}, &file); err != nil {
		return "", nil, err
	}
	filename := i.Reference.Object
	if extension := path.Ext(file.Path); adminFileExtension.MatchString(extension) {
		filename += extension
	}
	body, err := b.TG.DownloadResolved(ctx, file)
	return filename, body, err
}

func documentDelivered(observed botdelivery.Observation) bool {
	return observed.State == delivery.Succeeded && observed.MessageID > 0
}

func (b *Bot) renderBotFoodExport(ctx context.Context, i botdelivery.Intent) (string, []byte, error) {
	r := i.Reference
	exported, readErr := b.API.ExportFood(ctx, i.Owner, r.Event)
	if readErr != nil {
		return "", nil, readErr
	}
	filename, body := "food_orders_"+r.Event+".csv", exported.Orders
	if r.Family == botFamilyFoodSummaryExport {
		filename, body = "meal_summary_"+r.Event+".csv", exported.Summary
	}
	err := b.foodExportAllowed(ctx, i.Owner, r.Event)
	return filename, body, err
}
