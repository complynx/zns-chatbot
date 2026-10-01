package botdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
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

const (
	adminPageFamily            = "admin_page"
	adminPageSize              = 20
	adminPageMaxNavigationRows = adminPageSize + 3
)

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
	proof, err := s.adminPageIntake(ctx, tx, in)
	if err != nil {
		return err
	}
	generation := proof.Generation
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
	manifest, err := s.savedAdminPageManifest(ctx, tx, in, proof)
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

type adminPageIntakeProof struct {
	Generation int64
	Kind       string
	Control    string
	Digest     string
	MessageID  int64
}

func (s Service) adminPageIntake(
	ctx context.Context,
	tx pgx.Tx,
	in AdminPageResultsRequest,
) (adminPageIntakeProof, error) {
	var proof adminPageIntakeProof
	err := tx.QueryRow(ctx, `SELECT intake_generation,intake_kind,intake_control,intake_digest,intake_message_id
 FROM core.registration_ingress WHERE kind='telegram' AND bot_id=$1 AND request_key=$2 AND owner=''
 AND telegram_id=$3 AND intake_owner=$4 AND intake_generation IS NOT NULL FOR SHARE`,
		s.Delivery.BotID, strconv.FormatInt(in.Update, 10), in.Chat, in.Owner).
		Scan(&proof.Generation, &proof.Kind, &proof.Control, &proof.Digest, &proof.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, ErrBinding
	}
	return proof, core.DatabaseOperationError(err)
}

func (s Service) authorizeAdminPageIntake(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	proof adminPageIntakeProof,
) error {
	if proof.Kind == "callback" && (proof.Control == fmt.Sprintf("adminmsg:page:%d:%d", in.ID, in.Offset) ||
		(in.Offset == 0 && proof.Control == fmt.Sprintf("adminmsg:results:%d", in.ID))) {
		return nil
	}
	ok, err := adminPageManualMatches(ctx, tx, in, proof)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return s.authorizeAdminPageScript(ctx, tx, in, proof)
}

func adminPageManualMatches(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	proof adminPageIntakeProof,
) (bool, error) {
	if in.Offset != 0 {
		return false, nil
	}
	if proof.Kind == "message" {
		return adminPageAttachmentMatches(ctx, tx, in, proof, fmt.Sprintf("tg-admin-attach-%d", in.Update), 0)
	}
	if proof.Kind != "command" {
		return false, nil
	}
	var command string
	err := tx.QueryRow(ctx, `SELECT command FROM core.admin_messages WHERE id=$1 AND actor=$2 AND key=$3`,
		in.ID, in.Owner, fmt.Sprintf("tg-admin-%d", in.Update)).Scan(&command)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(command))) == proof.Digest, core.DatabaseOperationError(err)
}

func adminPageAttachmentMatches(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	proof adminPageIntakeProof, key string, inputID int64,
) (bool, error) {
	var html string
	err := tx.QueryRow(ctx, `SELECT s.text_html FROM core.admin_message_sources s
 JOIN core.admin_message_inputs i ON i.actor=s.actor AND i.attachment_key=s.key AND i.chat_id=s.chat_id
 WHERE s.actor=$1 AND s.key=$2 AND s.chat_id=$3 AND s.message_id=$4 AND i.state='attached'
 AND i.message_id=$5 AND ($6::bigint=0 OR i.id=$6)`, in.Owner, key, in.Chat, proof.MessageID, in.ID, inputID).Scan(&html)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(html))) == proof.Digest, core.DatabaseOperationError(err)
}

// Decode only the existing script receipt fields needed by this page route.
// agenthost depends on appclient, so importing its full record creates a cycle.
type adminPageScriptRecord struct {
	HistoryGeneration int64                 `json:"history_generation"`
	HistoryRedacted   bool                  `json:"history_redacted"`
	PassRedacted      bool                  `json:"pass_redacted"`
	MemoryRedacted    bool                  `json:"memory_redacted"`
	Calls             []adminPageScriptCall `json:"calls"`
}

type adminPageScriptCall struct {
	Source  *readsource.Derivation `json:"source"`
	Outcome struct {
		Name string `json:"name"`
	} `json:"outcome"`
	BroadcastReview *struct {
		Owner     string `json:"owner"`
		Chat      int64  `json:"chat"`
		Arguments struct {
			ID     int64 `json:"id"`
			Offset int64 `json:"offset"`
		} `json:"arguments"`
	} `json:"broadcast_review"`
	Broadcast *struct {
		Command    string                   `json:"command"`
		InputID    int64                    `json:"input_id"`
		Key        string                   `json:"key"`
		Attachment *adminmessage.Attachment `json:"attachment"`
	} `json:"broadcast"`
}

func (s Service) authorizeAdminPageScript(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	proof adminPageIntakeProof,
) error {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs' FOR SHARE`, in.Owner, in.Update).
		Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBinding
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	var records []adminPageScriptRecord
	if err = json.Unmarshal(raw, &records); err != nil {
		return err
	}
	for _, record := range records {
		if record.HistoryGeneration != proof.Generation || record.HistoryRedacted || record.PassRedacted ||
			record.MemoryRedacted {
			continue
		}
		ok, matchErr := adminPageScriptRecordMatches(ctx, tx, in, proof, record)
		if matchErr != nil {
			return matchErr
		}
		if ok {
			return nil
		}
	}
	return ErrBinding
}

func adminPageScriptRecordMatches(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	proof adminPageIntakeProof, record adminPageScriptRecord,
) (bool, error) {
	for _, call := range record.Calls {
		if call.Source == nil || !call.Source.Valid() || *call.Source.Generation != proof.Generation {
			continue
		}
		if review := call.BroadcastReview; call.Outcome.Name == "broadcasts.show" && review != nil &&
			review.Owner == in.Owner && review.Chat == in.Chat && review.Arguments.ID == in.ID && review.Arguments.Offset == in.Offset {
			return true, nil
		}
		ok, err := adminPageScriptPreviewMatches(ctx, tx, in, proof, call)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func adminPageScriptPreviewMatches(ctx context.Context, tx pgx.Tx, in AdminPageResultsRequest,
	proof adminPageIntakeProof, call adminPageScriptCall,
) (bool, error) {
	request := call.Broadcast
	if in.Offset != 0 || request == nil ||
		!strings.HasPrefix(request.Key, fmt.Sprintf("script-broadcast-%d-", in.Update)) {
		return false, nil
	}
	if call.Outcome.Name == "broadcasts.attach" && request.Attachment != nil && request.Attachment.ChatID == in.Chat {
		return adminPageAttachmentMatches(ctx, tx, in, proof, request.Key, request.InputID)
	}
	if call.Outcome.Name != "broadcasts.preview" || request.Attachment != nil {
		return false, nil
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.admin_messages WHERE id=$1 AND actor=$2 AND key=$3 AND command=$4)`,
		in.ID, in.Owner, request.Key, request.Command).
		Scan(&matches)
	return matches, core.DatabaseOperationError(err)
}

func (s Service) savedAdminPageManifest(
	ctx context.Context,
	tx pgx.Tx,
	in AdminPageResultsRequest,
	proof adminPageIntakeProof,
) (adminPageManifest, error) {
	final := fmt.Sprintf("admin_page:%d:%d", in.ID, in.Offset)
	operation, effect := ResultOperation(in.Owner, in.Update, final)
	kind := fmt.Sprintf("delivery_result:admin_page_manifest:%d:%s", s.Delivery.BotID, effect)
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, in.Owner, in.Update, kind).
		Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = s.authorizeAdminPageIntake(ctx, tx, in, proof); err != nil {
			return adminPageManifest{}, err
		}
		return s.createAdminPageManifest(ctx, tx, in, proof.Generation, operation, kind)
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
		saved.Offset != in.Offset || manifest.Generation != proof.Generation {
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
	for index, effect := range in.Effects {
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
		if effect.Effect != expected {
			return ErrBinding
		}
		if err := validateAdminPagePayload(in, effect, index); err != nil {
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

func validateAdminPagePayload(in AdminPageResultsRequest, effect AdminPageEffect, index int) error {
	payload := effect.Payload
	if payload.ChatID != in.Chat || payload.ParseMode != "" || payload.MessageID != 0 || payload.NativeMarkdown ||
		payload.LiteralPrefix != "" || payload.LiteralSuffix != "" {
		return ErrBinding
	}
	final := index == len(in.Effects)-1
	switch {
	case effect.Effect == fmt.Sprintf("admin_page:%d:%d", in.ID, in.Offset):
		if err := validateAdminPageNavigation(in, payload.Markup, final); err != nil {
			return err
		}
	case strings.HasPrefix(effect.Effect, "admin_view:"):
		if err := validateAdminPageActions(in.ID, payload.Markup, final); err != nil {
			return err
		}
	default:
		if len(payload.Markup.Rows) != 0 {
			return ErrBinding
		}
	}
	_, err := telegram.PrepareSend(payload)
	return err
}

func validAdminPageButton(button telegram.Button, data string) bool {
	return button.URL == "" && button.WebApp == nil && button.Data == data && len(button.Data) <= 64 &&
		strings.TrimSpace(button.Text) != "" && len(button.Text) <= 256
}

func validateAdminPageNavigation(in AdminPageResultsRequest, markup telegram.Markup, preparing bool) error {
	rows := markup.Rows
	if len(rows) > adminPageMaxNavigationRows {
		return ErrBinding
	}
	index := 0
	for index < len(rows) && len(rows[index]) == 1 && strings.HasPrefix(rows[index][0].Data, "adminmsg:inspect:") {
		if index >= adminPageSize || in.Offset > math.MaxInt64-int64(index) ||
			!validAdminPageButton(
				rows[index][0],
				fmt.Sprintf("adminmsg:inspect:%d:%d", in.ID, in.Offset+int64(index)),
			) {
			return ErrBinding
		}
		index++
	}
	items := index
	if in.Offset > 0 {
		if index >= len(rows) || len(rows[index]) != 1 ||
			!validAdminPageButton(
				rows[index][0],
				fmt.Sprintf("adminmsg:page:%d:%d", in.ID, max(0, in.Offset-adminPageSize)),
			) {
			return ErrBinding
		}
		index++
	}
	if index < len(rows) && len(rows[index]) == 1 && strings.HasPrefix(rows[index][0].Data, "adminmsg:page:") {
		if items == 0 || in.Offset > math.MaxInt64-int64(items) ||
			!validAdminPageButton(rows[index][0], fmt.Sprintf("adminmsg:page:%d:%d", in.ID, in.Offset+int64(items))) {
			return ErrBinding
		}
		index++
	}
	if !preparing {
		if index != len(rows) {
			return ErrBinding
		}
		return nil
	}
	if index != len(rows)-1 || len(rows[index]) != 2 ||
		!validAdminPageButton(rows[index][0], fmt.Sprintf("adminmsg:results:%d", in.ID)) ||
		!validAdminPageButton(rows[index][1], fmt.Sprintf("adminmsg:cancel:%d", in.ID)) {
		return ErrBinding
	}
	return nil
}

func validateAdminPageActions(id int64, markup telegram.Markup, final bool) error {
	if !final {
		if len(markup.Rows) != 0 {
			return ErrBinding
		}
		return nil
	}
	actions := []string{"send", "results", "cancel"}
	if len(markup.Rows) != len(actions) {
		return ErrBinding
	}
	for index, action := range actions {
		row := markup.Rows[index]
		if len(row) != 1 || !validAdminPageButton(row[0], fmt.Sprintf("adminmsg:%s:%d", action, id)) {
			return ErrBinding
		}
	}
	return nil
}
