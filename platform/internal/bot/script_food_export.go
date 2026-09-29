package bot

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const foodExportRequestKind = "food_export_request"
const maxFoodExportContinuation = 128

type foodExportRequest struct {
	ID       string `json:"id"`
	EventID  string `json:"event_id"`
	UpdateID int64  `json:"update_id"`
	ChatID   int64  `json:"chat_id"`
}

type foodExportResult struct {
	Continuation string `json:"continuation"`
	Complete     bool   `json:"complete"`
}

func (b *Bot) prepareFoodExport(
	ctx context.Context,
	owner string,
	update int64,
	call scriptclient.ToolCall,
	record scriptToolRecord,
) (scriptToolRecord, error) {
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

func (b *Bot) executeFoodExport(ctx context.Context, owner string, record scriptToolRecord) (any, error) {
	request := record.FoodExport
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner || request.ChatID != source.in.chat {
		return nil, errors.New("food export binding missing")
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
	err = b.exportFoodEvent(ctx, source.in, request.UpdateID, request.EventID)
	result.Complete = err == nil
	// A failed external delivery remains resumable with the same host receipt ID.
	return result, nil
}

func (b *Bot) foodExportAllowed(ctx context.Context, owner, event string) error {
	var capability legacyfood.OwnerCapabilities
	err := b.API.call(
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
) (foodExportRequest, error) {
	request := foodExportRequest{}
	if continuation != "" {
		err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND kind=$2 AND content->>'id'=$3`, owner, foodExportRequestKind, continuation).
			Scan(&request)
		if err != nil {
			return request, errors.New("food export continuation unavailable")
		}
		return request, nil
	}
	capability, err := b.API.foodCapabilities(ctx, owner)
	if err != nil {
		return request, err
	}
	if !capability.CanExport {
		return request, errors.New("food export unavailable")
	}
	request = foodExportRequest{ID: rand.Text(), EventID: capability.EventID, UpdateID: update, ChatID: chat}
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
