package bot

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const foodExportRequestKind = "food_export_request"
const maxFoodExportContinuation = 128

type foodExportResult struct {
	Continuation string `json:"continuation"`
	Complete     bool   `json:"complete"`
}

func (b *Bot) prepareFoodExport(
	ctx context.Context,
	owner string,
	update int64,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
) (agenthost.ScriptToolRecord, error) {
	var args struct {
		Continuation string `json:"continuation"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	if len(args.Continuation) > maxFoodExportContinuation {
		return record, errors.New("invalid food export continuation")
	}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return record, errors.New("food delivery context missing")
	}
	request, err := b.loadFoodExportRequest(ctx, owner, update, source.in.chat, args.Continuation)
	if err != nil {
		return record, err
	}
	if request.ChatID != source.in.chat {
		return record, errors.New("food export chat changed")
	}
	record.FoodExport = &request
	return record, nil
}

func (b *Bot) executeFoodExport(ctx context.Context, owner string, record agenthost.ScriptToolRecord) (any, error) {
	request := record.FoodExport
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner || request.ChatID != source.in.chat {
		return nil, errors.New("food export binding missing")
	}
	if record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("missing admitted source")
	}
	if err := b.bindFoodExportSource(ctx, owner, request, record.Source); err != nil {
		return nil, err
	}
	if err := b.checkFoodDeliverySource(ctx, owner, record.Source); err != nil {
		return nil, err
	}
	if err := b.foodExportAllowed(ctx, owner, request.EventID); err != nil {
		return nil, err
	}
	result := foodExportResult{Continuation: request.ID}
	// Serialize continuation deliveries across updates without locking Core state.
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, foodExportRequestKind+":"+owner+":"+request.ID).
		Scan(&locked)
	if err != nil {
		return nil, err
	}
	if !locked {
		return result, nil
	}
	err = b.exportFoodEvent(ctx, source.in, request.UpdateID, request.EventID, record.Source)
	if err != nil {
		return result, err
	}
	err = b.DB.QueryRow(ctx, `SELECT count(*)=2 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind IN ('food_orders_export','food_summary_export')`, owner, request.UpdateID).
		Scan(&result.Complete)
	if err != nil {
		return result, err
	}
	// A failed external delivery remains resumable with the same host receipt ID.
	return result, nil
}

func (b *Bot) foodExportAllowed(ctx context.Context, owner, event string) error {
	var capability legacyfood.OwnerCapabilities
	err := b.API.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/food/capabilities?event="+url.QueryEscape(event),
		nil,
		&capability,
	)
	if err != nil {
		return err
	}
	if capability.EventID != event || !capability.CanExport {
		return errors.New("food export unavailable")
	}
	return nil
}

func (b *Bot) loadFoodExportRequest(
	ctx context.Context,
	owner string,
	update, chat int64,
	continuation string,
) (agenthost.FoodExportRequest, error) {
	request := agenthost.FoodExportRequest{}
	if continuation != "" {
		err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND kind=$2 AND content->>'id'=$3`, owner, foodExportRequestKind, continuation).
			Scan(&request)
		if err != nil {
			return request, errors.New("food export continuation unavailable")
		}
		if request.Source == nil || !request.Source.Valid() {
			return request, errors.New("food export source missing")
		}
		return request, nil
	}
	capability, err := b.API.FoodCapabilities(ctx, owner)
	if err != nil {
		return request, err
	}
	if !capability.CanExport {
		return request, errors.New("food export unavailable")
	}
	request = agenthost.FoodExportRequest{ID: rand.Text(), EventID: capability.EventID, UpdateID: update, ChatID: chat}
	_, err = b.DB.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		owner,
		update,
		foodExportRequestKind,
		request,
	)
	if err != nil {
		return request, err
	}
	err = b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, update, foodExportRequestKind).
		Scan(&request)
	return request, err
}

// Persist the admitted source once. A continuation cannot replace its original evidence.
func (b *Bot) bindFoodExportSource(
	ctx context.Context,
	owner string,
	request *agenthost.FoodExportRequest,
	source *readsource.Derivation,
) error {
	if source == nil || !source.Valid() {
		return errors.New("missing admitted source")
	}
	detached := source.Clone()
	_, err := b.DB.Exec(ctx, `UPDATE bot.interactions SET content=jsonb_set(content,'{source}',$4::jsonb)
 WHERE owner=$1 AND update_id=$2 AND kind=$3 AND (content->'source' IS NULL OR content->'source'='null'::jsonb)`, owner, request.UpdateID, foodExportRequestKind, detached)
	if err != nil {
		return err
	}
	return b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, request.UpdateID, foodExportRequestKind).
		Scan(request)
}

func (b *Bot) foodExportSource(ctx context.Context, owner string, update int64) (*readsource.Derivation, bool, error) {
	var request agenthost.FoodExportRequest
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, update, foodExportRequestKind).
		Scan(&request)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if request.Source == nil || !request.Source.Valid() {
		return nil, true, errors.New("food export source missing")
	}
	detached := request.Source.Clone()
	return &detached, true, nil
}

// This is the last local exposure check, not an atomic fence across Telegram.
func (b *Bot) checkFoodDeliverySource(ctx context.Context, owner string, source *readsource.Derivation) error {
	if source == nil || !source.Valid() {
		return errors.New("missing admitted source")
	}
	if err := b.Host.CheckReadAuthorities(ctx, owner, source.Authorities); err != nil {
		return err
	}
	return b.API.CheckHistoryGeneration(ctx, owner, *source.Generation)
}
