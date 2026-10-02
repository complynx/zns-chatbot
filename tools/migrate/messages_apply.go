package migrate

import (
	"context"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type MessageApplySummary struct {
	MessageValidationSummary

	Applied    int  `json:"applied"`
	Reused     int  `json:"reused"`
	Tombstoned int  `json:"tombstoned"`
	Reconciled bool `json:"reconciled"`
}

func ApplyMessages(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
) (MessageApplySummary, error) {
	return importMessages(ctx, dsn, stage, plan, resolutions, limits, false)
}

func ReconcileMessages(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
) (MessageApplySummary, error) {
	return importMessages(ctx, dsn, stage, plan, resolutions, limits, true)
}

func importMessages(
	ctx context.Context,
	dsn, stage, plan, resolutions string,
	limits Limits,
	verifyOnly bool,
) (MessageApplySummary, error) {
	var result MessageApplySummary
	p, err := prepareMessages(stage, plan, resolutions, limits)
	if err != nil {
		return result, err
	}
	result.MessageValidationSummary = messageSummary(p)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return result, errors.New("apply_database_unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return result, errors.New("apply_transaction_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// The operator stops runtime writers. Lock once for the complete history import.
	if _, err = tx.Exec(ctx, `LOCK TABLE core.conversation_events IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return result, errors.New("message_schema_unavailable")
	}
	if err = messagePreexisting(ctx, tx, p); err != nil {
		return result, err
	}
	if !verifyOnly {
		if _, err = tx.Exec(
			ctx,
			`CREATE SCHEMA IF NOT EXISTS migrate_import; CREATE TABLE IF NOT EXISTS migrate_import.message_receipts(source_key text PRIMARY KEY REFERENCES core.legacy_message_references(source_key), plan_sha256 text NOT NULL, resolution_sha256 text NOT NULL)`,
		); err != nil {
			return result, errors.New("apply_schema_unavailable")
		}
	}
	var applied, reusedCount, tombstoneCount int
	for _, row := range p.rows {
		reused, tombstoned, applyErr := applyOneMessage(ctx, tx, p, row, verifyOnly)
		if applyErr != nil {
			return result, applyErr
		}
		if reused {
			reusedCount++
		} else {
			applied++
		}
		if tombstoned {
			tombstoneCount++
		}
	}
	if err = messageOrder(ctx, tx, p); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, errors.New("apply_commit_failed")
	}
	result.Applied, result.Reused, result.Tombstoned = applied, reusedCount, tombstoneCount
	result.Reconciled = true
	return result, nil
}

func applyOneMessage(
	ctx context.Context,
	tx pgx.Tx,
	p preparedMessages,
	row preparedMessage,
	verifyOnly bool,
) (bool, bool, error) {
	exists, tombstone, err := reconcileMessage(ctx, tx, p, row)
	if err != nil {
		return false, false, err
	}
	if exists {
		if !verifyOnly {
			return false, false, errors.New("message_reference_conflict")
		}
		return true, tombstone, nil
	}
	if verifyOnly {
		return false, false, errors.New("message_reference_missing")
	}
	if err = messageOwner(ctx, tx, p, row); err != nil {
		return false, false, err
	}
	eventID, err := insertMessageEvent(ctx, tx, row)
	if err != nil {
		return false, false, err
	}
	var owner *string
	if row.decision.Owner != "" {
		owner = &row.decision.Owner
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.legacy_message_references(source_key,event_id,owner,bot_namespace,collection,identity_sha256,source_record_sha256,resolved_at,plan_sha256,resolution_sha256,disposition) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		row.record.Legacy.Key,
		eventID,
		owner,
		strconv.FormatInt(p.plan.BotID, 10),
		row.record.Legacy.Collection,
		hashBytes([]byte(row.identity)),
		row.record.Legacy.RecordSHA256,
		row.instant,
		p.planHash,
		p.resolutionHash,
		row.decision.Disposition,
	)
	if err != nil {
		return false, false, errors.New("message_reference_insert_failed")
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO migrate_import.message_receipts(source_key,plan_sha256,resolution_sha256) VALUES($1,$2,$3)`,
		row.record.Legacy.Key,
		p.planHash,
		p.resolutionHash,
	); err != nil {
		return false, false, errors.New("message_receipt_insert_failed")
	}
	if _, _, err = reconcileMessage(ctx, tx, p, row); err != nil {
		return false, false, err
	}
	return false, false, nil
}
func messageText(row preparedMessage) (string, string) {
	if row.decision.Disposition == messageOmitted {
		return "", row.decision.Provenance
	}
	text := row.record.Candidate.Content
	if len(text) > messageEventTextBytes {
		text = text[:4988]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	if len(row.record.Candidate.Content) > messageEventTextBytes {
		text += "\n[continued]"
	}
	return text, ""
}
func messageOwner(ctx context.Context, tx pgx.Tx, p preparedMessages, row preparedMessage) error {
	if row.decision.Owner == "" {
		return nil
	}
	var owner string
	err := tx.QueryRow(ctx, `SELECT t.owner FROM core.telegram_identities t JOIN core.users u ON u.id=t.owner WHERE t.bot_id=$1 AND t.telegram_id=$2 AND u.telegram_id=$2 FOR SHARE OF t,u`, p.plan.BotID, row.record.Candidate.TelegramID).
		Scan(&owner)
	if err != nil || owner != row.decision.Owner {
		return errors.New("message_owner_mismatch")
	}
	return nil
}

// Reject conversation rows outside this exact run while writers are stopped.
func messagePreexisting(ctx context.Context, tx pgx.Tx, p preparedMessages) error {
	var unmatched bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.conversation_events e WHERE e.owner=ANY($1::text[]) AND NOT EXISTS(SELECT 1 FROM core.legacy_message_references r WHERE r.event_id=e.id AND r.plan_sha256=$2 AND r.resolution_sha256=$3))`, p.owners, p.planHash, p.resolutionHash).
		Scan(&unmatched)
	if err != nil || unmatched {
		return errors.New("message_preexisting_history")
	}
	return nil
}

// The whole committed history must retain the prepared chronological order.
func messageOrder(ctx context.Context, tx pgx.Tx, p preparedMessages) error {
	var previous int64
	for _, row := range p.rows {
		var id *int64
		if err := tx.QueryRow(ctx, `SELECT event_id FROM core.legacy_message_references WHERE source_key=$1`, row.record.Legacy.Key).
			Scan(&id); err != nil {
			return errors.New("message_reference_unavailable")
		}
		if id != nil {
			if *id <= previous {
				return errors.New("message_committed_order_invalid")
			}
			previous = *id
		}
	}
	return nil
}

func insertMessageEvent(ctx context.Context, tx pgx.Tx, row preparedMessage) (*int64, error) {
	var err error
	var eventID *int64
	if row.decision.Disposition != messageExcluded {
		excerpt, omission := messageText(row)
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO core.conversation_events(owner,source_key,kind,text,omitted,omission_reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, row.decision.Owner, "legacy-message:"+row.record.Legacy.Key, row.record.Candidate.Role, excerpt, row.decision.Disposition == messageOmitted, omission, row.instant).
			Scan(&id)
		if err != nil {
			return nil, errors.New("message_insert_failed")
		}
		eventID = &id
		if row.decision.Disposition == messageRetained && len(row.record.Candidate.Content) > messageEventTextBytes {
			body := row.record.Candidate.Content
			if _, err = tx.Exec(
				ctx,
				`INSERT INTO core.conversation_message_bodies(event_id,body,body_sha256,character_count) VALUES($1,$2,$3,$4)`,
				id,
				body,
				hashBytes([]byte(body)),
				utf8.RuneCountInString(body),
			); err != nil {
				return nil, errors.New("message_body_insert_failed")
			}
		}
	}

	return eventID, nil
}
