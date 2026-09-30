package migrate

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Reconciliation survives removal of temporary receipts. Tombstones are terminal
// content states and are never repaired from an older private archive.
func reconcileMessage(ctx context.Context, tx pgx.Tx, p preparedMessages, row preparedMessage) (bool, bool, error) {
	var eventID *int64
	var matches, tombstone bool
	err := tx.QueryRow(ctx, `SELECT event_id,tombstoned,coalesce(owner,'')=$2 AND bot_namespace=$3 AND collection=$4 AND identity_sha256=$5 AND source_record_sha256=$6 AND resolved_at=$7 AND plan_sha256=$8 AND resolution_sha256=$9 AND disposition=$10 FROM core.legacy_message_references WHERE source_key=$1 FOR SHARE`, row.record.Legacy.Key, row.decision.Owner, strconv.FormatInt(p.plan.BotID, 10), row.record.Legacy.Collection, hashBytes([]byte(row.identity)), row.record.Legacy.RecordSHA256, row.instant, p.planHash, p.resolutionHash, row.decision.Disposition).
		Scan(&eventID, &tombstone, &matches)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil || !matches {
		return false, false, errors.New("message_reference_conflict")
	}
	if err = messageOwner(ctx, tx, p, row); err != nil {
		return false, false, err
	}
	if row.decision.Disposition == messageExcluded {
		if eventID != nil || tombstone {
			return false, false, errors.New("message_exclusion_conflict")
		}
		return true, false, nil
	}
	if eventID == nil {
		return false, false, errors.New("message_event_missing")
	}
	text, omission := messageText(row)
	omitted := row.decision.Disposition == messageOmitted
	if tombstone {
		text = ""
		omission = "deleted"
		omitted = true
	}
	var bodyMatches bool
	hasBody := row.decision.Disposition == messageRetained &&
		len(row.record.Candidate.Content) > messageEventTextBytes &&
		!tombstone
	err = tx.QueryRow(ctx, `SELECT owner=$2 AND source_key=$3 AND kind=$4 AND created_at=$5 AND text=$6 AND omitted=$7 AND omission_reason=$8 AND details='{}'::jsonb AND origin='original',
 CASE WHEN $9 THEN EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=e.id AND b.body_sha256=$10 AND b.body=$11 AND b.character_count=char_length($11)) ELSE NOT EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=e.id) END
 FROM core.conversation_events e WHERE id=$1`, *eventID, row.decision.Owner, "legacy-message:"+row.record.Legacy.Key, row.record.Candidate.Role, row.instant, text, omitted, omission, hasBody, hashBytes([]byte(row.record.Candidate.Content)), row.record.Candidate.Content).
		Scan(&matches, &bodyMatches)
	if err != nil || !matches || !bodyMatches {
		return false, false, errors.New("message_content_conflict")
	}
	return true, tombstone, nil
}
