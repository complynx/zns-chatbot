package botdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type AdminPageEffect struct {
	Effect  string        `json:"Effect"`
	Payload telegram.Send `json:"Payload"`
}

type AdminPageResultsRequest struct {
	Owner   string            `json:"Owner"`
	Chat    int64             `json:"Chat"`
	Update  int64             `json:"Update"`
	ID      int64             `json:"ID"`
	Offset  int64             `json:"Offset"`
	Effects []AdminPageEffect `json:"Effects"`
}

const adminPageFamily = "admin_page"

type adminPageManifest struct {
	Generation int64                   `json:"generation"`
	Request    AdminPageResultsRequest `json:"request"`
}

// EnqueueAdminPageResults freezes the complete page and action card before any
// effect becomes visible to the worker. The private manifest uses the existing
// delivery-result generation fence and erasure trigger.
func (s Service) EnqueueAdminPageResults(ctx context.Context, in AdminPageResultsRequest) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	if err := validateAdminPageResults(in); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM core.conversation_history_generations WHERE owner=$1),0)`, in.Owner).
		Scan(&generation); err != nil {
		return core.DatabaseOperationError(err)
	}
	ref := Reference{
		Kind:       ResultIntent,
		Family:     adminPageFamily,
		Version:    in.ID,
		Generation: &generation,
		ResultKind: "admin_page_manifest",
	}
	// Follow the same actor/source/generation lock order as individual results.
	if err = s.lockSource(
		ctx,
		tx,
		Intent{BotID: s.Delivery.BotID, Owner: in.Owner, Chat: in.Chat, Reference: ref},
	); err != nil {
		return err
	}
	manifest, err := s.savedAdminPageManifest(ctx, tx, in, generation)
	if err != nil {
		return err
	}
	if err = fence.LockGeneration(ctx, tx, in.Owner, &manifest.Generation); err != nil {
		return err
	}
	for _, effect := range manifest.Request.Effects {
		if err = s.enqueueResultTx(ctx, tx, ResultRequest{
			Owner: in.Owner, Chat: in.Chat, Update: in.Update, Effect: effect.Effect,
			Reference: Reference{Family: adminPageFamily, Version: in.ID, Generation: &manifest.Generation},
			Result:    StoredResult{Generation: manifest.Generation, Payload: effect.Payload},
		}); err != nil {
			return err
		}
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) savedAdminPageManifest(
	ctx context.Context,
	tx pgx.Tx,
	in AdminPageResultsRequest,
	generation int64,
) (adminPageManifest, error) {
	final := fmt.Sprintf("admin_page:%d:%d", in.ID, in.Offset)
	operation, effect := ResultOperation(in.Owner, in.Update, final)
	kind := fmt.Sprintf("delivery_result:admin_page_manifest:%d:%s", s.Delivery.BotID, effect)
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, in.Owner, in.Update, kind).
		Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.createAdminPageManifest(ctx, tx, in, generation, operation, kind)
	}
	if err != nil {
		return adminPageManifest{}, core.DatabaseOperationError(err)
	}
	var manifest adminPageManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return manifest, err
	}
	saved := manifest.Request
	if saved.Owner != in.Owner || saved.Chat != in.Chat || saved.Update != in.Update || saved.ID != in.ID ||
		saved.Offset != in.Offset {
		return manifest, ErrBinding
	}
	return manifest, validateAdminPageResults(saved)
}

func (s Service) createAdminPageManifest(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	generation int64, operation, kind string,
) (adminPageManifest, error) {
	// Erased manifests and legacy partial result sets cannot authorize a new
	// snapshot for an already persisted ingress.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2)`, s.Delivery.BotID, operation).
		Scan(&exists); err != nil {
		return adminPageManifest{}, core.DatabaseOperationError(err)
	}
	if exists {
		return adminPageManifest{}, ErrStale
	}
	manifest := adminPageManifest{Generation: generation, Request: in}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return adminPageManifest{}, err
	}
	if len(raw) > 1<<20 {
		return adminPageManifest{}, ErrBinding
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)`,
		in.Owner,
		in.Update,
		kind,
		raw,
	)
	return manifest, core.DatabaseOperationError(err)
}

func validateAdminPageResults(in AdminPageResultsRequest) error {
	// Twenty rows each have at most 1000 failure bytes and bounded destinations.
	// Both the page and its action-card copy plus 4096 content units fit in 64
	// chunks of 1800 runes. The byte cap also matches stored private results.
	if in.Owner == "" || in.Chat <= 0 || in.Update < 0 || in.ID <= 0 || in.Offset < 0 || len(in.Effects) == 0 ||
		len(in.Effects) > 64 {
		return ErrBinding
	}
	final := fmt.Sprintf("admin_page:%d:%d", in.ID, in.Offset)
	pageDone, viewIndex, chunkIndex := false, 0, 0
	for _, effect := range in.Effects {
		expected := fmt.Sprintf("%s:chunk:%d", final, chunkIndex)
		switch {
		case pageDone:
			expected = fmt.Sprintf("admin_view:%d:%d", in.ID, viewIndex)
			viewIndex++
		case effect.Effect == final:
			expected, pageDone = final, true
		default:
			chunkIndex++
		}
		if effect.Effect != expected || effect.Payload.ChatID != in.Chat || effect.Payload.ParseMode != "" ||
			effect.Payload.MessageID != 0 {
			return ErrBinding
		}
		if strings.HasPrefix(effect.Effect, final+":chunk:") && len(effect.Payload.Markup.Rows) != 0 {
			return ErrBinding
		}
		if _, err := telegram.PrepareSend(effect.Payload); err != nil {
			return err
		}
	}
	if !pageDone {
		return ErrBinding
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return ErrBinding
	}
	return nil
}
