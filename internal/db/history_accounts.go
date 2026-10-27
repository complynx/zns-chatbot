package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

// HistoryAccountMapping selects an applied users record and an independently
// reconciled IAM subject. Historical IAM fields are never used as authority.
type HistoryAccountMapping struct {
	RecordID uuid.UUID `json:"record_id"`
	Subject  string    `json:"subject"`
}

type HistoryAccountsRequest struct {
	ArchiveID uuid.UUID               `json:"archive_id"`
	SHA256    string                  `json:"sha256"`
	Mappings  []HistoryAccountMapping `json:"mappings"`
}

// HistoryAccountBinding is derived from validated source data, not caller input.
type HistoryAccountBinding struct {
	BotID        int64  `json:"bot_id"`
	TelegramID   int64  `json:"telegram_id"`
	Organization string `json:"organization"`
	Subject      string `json:"subject"`
}

type HistoryAccountReceipt struct {
	RecordID   uuid.UUID             `json:"record_id"`
	ArchiveID  uuid.UUID             `json:"archive_id"`
	UserID     uuid.UUID             `json:"user_id"`
	Binding    HistoryAccountBinding `json:"binding"`
	Created    bool                  `json:"created"`
	ImportedAt time.Time             `json:"imported_at"`
}

type historyAccountSource struct {
	record  legacyimport.Record
	profile json.RawMessage
}

// ImportHistoryAccounts is an operator-only offline seam, with no public route.
// The caller must verify exact approval and reconcile current IAM accounts before
// calling. verify must recheck operator authority and every exact binding against
// IAM before commit, including replays. It must not reuse this database pool.
// A batch is atomic and bounded to 1000 accounts. Source record IDs are durable
// idempotency keys. Existing profiles, preferences and Telegram metadata survive.
// This creates no conversations, tasks, jobs, live business or confirmed names.
func (s *Store) ImportHistoryAccounts(ctx context.Context, scope ProfileScope, request HistoryAccountsRequest,
	verify func(context.Context, []HistoryAccountBinding) error,
) ([]HistoryAccountReceipt, error) {
	if verify == nil || len(request.Mappings) == 0 || len(request.Mappings) > 1000 {
		return nil, ErrInvalidHistory
	}
	if _, err := historyApplyHash(HistoryApplyArguments{ArchiveID: request.ArchiveID,
		SHA256: request.SHA256, ProjectionVersion: legacyimport.ProjectionVersion}); err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	for _, mapping := range request.Mappings {
		if mapping.RecordID == uuid.Nil || !prelinkText(mapping.Subject, prelinkFieldLimit) || seen[mapping.RecordID] {
			return nil, ErrInvalidHistory
		}
		seen[mapping.RecordID] = true
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("history-accounts:%d:%s", scope.BotID, scope.Organization),
	); err != nil {
		return nil, err
	}
	sources, err := readHistoryAccountSources(ctx, tx, q, scope, request)
	if err != nil {
		return nil, err
	}
	receipts := make([]HistoryAccountReceipt, 0, len(request.Mappings))
	bindings := make([]HistoryAccountBinding, 0, len(request.Mappings))
	for _, mapping := range request.Mappings {
		source := sources[mapping.RecordID]
		binding := HistoryAccountBinding{BotID: scope.BotID, TelegramID: source.record.OwnerID,
			Organization: scope.Organization, Subject: mapping.Subject}
		receipt, importErr := importHistoryAccount(ctx, tx, q, request, mapping, binding, source.profile)
		if importErr != nil {
			return nil, importErr
		}
		receipts = append(receipts, receipt)
		bindings = append(bindings, binding)
	}
	if err = verify(ctx, bindings); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipts, nil
}

func readHistoryAccountSources(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope ProfileScope,
	request HistoryAccountsRequest,
) (map[uuid.UUID]historyAccountSource, error) {
	archive, err := q.GetLegacyArchive(ctx, sqlc.GetLegacyArchiveParams{ID: request.ArchiveID,
		BotID: scope.BotID, OrganizationID: scope.Organization})
	if err != nil {
		return nil, err
	}
	if archive.Sha256 != request.SHA256 {
		return nil, ErrConflict
	}
	sources := make(map[uuid.UUID]historyAccountSource, len(request.Mappings))
	for _, mapping := range request.Mappings {
		var source historyAccountSource
		var hash []byte
		err = tx.QueryRow(ctx, `SELECT h.source_instance,h.collection_name,h.source_key,h.source_role,
h.owner_telegram_id,h.canonical_hash,m.source_line
FROM shuttle_legacy_history_records h JOIN shuttle_legacy_history_memberships m ON m.record_id=h.id
WHERE h.id=$1 AND m.archive_id=$2 AND h.bot_id=$3 AND h.organization_id=$4
AND h.source_role='users' AND h.projection_version=$5`, mapping.RecordID, request.ArchiveID,
			scope.BotID, scope.Organization, legacyimport.ProjectionVersion).Scan(&source.record.SourceInstance,
			&source.record.Collection, &source.record.SourceKey, &source.record.Role, &source.record.OwnerID,
			&hash, &source.record.Line)
		if err != nil {
			return nil, err
		}
		copy(source.record.SHA256[:], hash)
		sources[mapping.RecordID] = source
	}
	reader, err := newLegacyArchiveReader(ctx, q, archive)
	if err != nil {
		return nil, err
	}
	observed := legacyInputReader{source: reader}
	found := 0
	validation, err := legacyimport.VisitBundle(ctx, &observed, archive.Sha256, func(record legacyimport.Record) error {
		matched, matchErr := projectHistoryAccountSource(record, sources)
		if matched {
			found++
		}
		return matchErr
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if observed.err != nil {
		return nil, observed.err
	}
	if err != nil {
		return nil, err
	}
	if found != len(sources) || validation.SourceBotID != scope.BotID ||
		validation.Archive.SizeBytes != archive.SizeBytes {
		return nil, ErrConflict
	}
	return sources, nil
}

func importHistoryAccount(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, request HistoryAccountsRequest,
	mapping HistoryAccountMapping, binding HistoryAccountBinding, profile json.RawMessage,
) (HistoryAccountReceipt, error) {
	raw, err := json.Marshal(struct {
		ArchiveID uuid.UUID             `json:"archive_id"`
		SHA256    string                `json:"sha256"`
		Binding   HistoryAccountBinding `json:"binding"`
	}{request.ArchiveID, request.SHA256, binding})
	if err != nil {
		return HistoryAccountReceipt{}, err
	}
	hash := sha256.Sum256(raw)
	var priorHash, priorReceipt []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,receipt FROM shuttle_history_account_receipts WHERE record_id=$1`,
		mapping.RecordID).Scan(&priorHash, &priorReceipt)
	replay := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return HistoryAccountReceipt{}, err
	}
	if replay && !bytes.Equal(priorHash, hash[:]) {
		return HistoryAccountReceipt{}, ErrConflict
	}
	proposedID := uuid.New()
	owner, err := q.EnsureTelegramUser(ctx, sqlc.EnsureTelegramUserParams{ID: proposedID, BotID: binding.BotID,
		TgUserID: binding.TelegramID, Profile: profile})
	if err != nil {
		return HistoryAccountReceipt{}, err
	}
	owner, err = q.LockTelegramUser(ctx, sqlc.LockTelegramUserParams{ID: owner.ID, BotID: binding.BotID})
	if err != nil {
		return HistoryAccountReceipt{}, err
	}
	if owner.Revoked || (owner.ZitadelOrg != "" && owner.ZitadelOrg != binding.Organization) ||
		(owner.ZitadelSubject != "" && owner.ZitadelSubject != binding.Subject) {
		return HistoryAccountReceipt{}, ErrConflict
	}
	if replay {
		return replayHistoryAccount(priorReceipt, owner, binding)
	}
	if _, err = q.BindIdentity(ctx, sqlc.BindIdentityParams{ID: owner.ID, ZitadelSubject: binding.Subject,
		ZitadelOrg: binding.Organization}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return HistoryAccountReceipt{}, ErrConflict
		}
		return HistoryAccountReceipt{}, err
	}
	receipt := HistoryAccountReceipt{RecordID: mapping.RecordID, ArchiveID: request.ArchiveID,
		UserID: owner.ID, Binding: binding, Created: owner.ID == proposedID, ImportedAt: time.Now().UTC()}
	raw, err = json.Marshal(receipt)
	if err != nil {
		return receipt, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_history_account_receipts(record_id,user_id,request_hash,receipt)
VALUES($1,$2,$3,$4)`, mapping.RecordID, owner.ID, hash[:], raw)
	return receipt, err
}

func projectHistoryAccountSource(record legacyimport.Record, sources map[uuid.UUID]historyAccountSource) (bool, error) {
	for id, source := range sources {
		target := source.record
		if record.SourceInstance != target.SourceInstance || record.Collection != target.Collection ||
			record.SourceKey != target.SourceKey {
			continue
		}
		if record.Role != target.Role || record.OwnerID != target.OwnerID || record.Line != target.Line ||
			record.SHA256 != target.SHA256 {
			return false, ErrConflict
		}
		var projection struct {
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if len(record.Projection) > 0 {
			if err := json.Unmarshal(record.Projection, &projection); err != nil {
				return false, err
			}
		}
		// Only the established historical whitelist becomes Telegram hints.
		source.profile, _ = json.Marshal(projection.Fields)
		if projection.Fields == nil {
			source.profile = json.RawMessage(`{}`)
		}
		sources[id] = source
		return true, nil
	}
	return false, nil
}

func replayHistoryAccount(
	raw []byte,
	owner sqlc.ShuttleUser,
	binding HistoryAccountBinding,
) (HistoryAccountReceipt, error) {
	var receipt HistoryAccountReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return receipt, err
	}
	if receipt.UserID != owner.ID || owner.ZitadelOrg != binding.Organization ||
		owner.ZitadelSubject != binding.Subject {
		return receipt, ErrConflict
	}
	return receipt, nil
}
