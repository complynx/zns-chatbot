package adminmessage

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// JobProgress summarizes the complete saved audience, independent of paging.
// Outcome counters reflect durable records; SharedPaused observes shared pacing.
// Neither establishes current send eligibility or retry rights.
type JobProgress struct {
	Total     int64 `json:"total"`
	NotQueued int64 `json:"not_queued"`
	Queued    int64 `json:"queued"`
	Deferred  int64 `json:"deferred"`
	Sending   int64 `json:"sending"`
	Succeeded int64 `json:"succeeded"`
	Rejected  int64 `json:"rejected"`
	Cancelled int64 `json:"cancelled"`
	Uncertain int64 `json:"uncertain"`
	Parked    int64 `json:"parked"`
	Paused    int64 `json:"paused"`
	// SharedPaused is an overlapping subset of queued/deferred recipients.
	// It observes shared pacing only and does not imply send eligibility.
	SharedPaused int64 `json:"shared_paused"`
}

type progressDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// readJobProgress runs after owned() in the same authorized review transaction.
// SQL aggregates only metadata; no audience payload or identifier is returned.
func readJobProgress(ctx context.Context, db progressDatabase, id int64) (JobProgress, error) {
	var result JobProgress
	var unknown int64
	err := db.QueryRow(ctx, jobProgressSQL, id).Scan(&result.Total, &result.NotQueued, &result.Queued,
		&result.Deferred, &result.Sending, &result.Succeeded, &result.Rejected, &result.Cancelled,
		&result.Uncertain, &result.Parked, &result.Paused, &result.SharedPaused, &unknown)
	if err != nil {
		return JobProgress{}, core.DatabaseOperationContextError(ctx, err)
	}
	if unknown != 0 {
		return JobProgress{}, problem(http.StatusInternalServerError, "admin_message_progress_invalid")
	}
	return result, nil
}

// A pending row with a saved failure is a deferred attempt, including after its
// deadline becomes due. A date alone does not establish a failure or retry right.
const jobProgressSQL = `WITH outcomes AS (
 SELECT CASE
 WHEN d.id IS NULL THEN CASE WHEN m.state='cancelled' THEN 'cancelled' ELSE 'not_queued' END
 WHEN d.state='pending' AND d.failure<>'' THEN 'deferred'
 ELSE d.state END AS state,
 (d.state='pending' AND q.state='pending' AND
 (COALESCE(b.pause_reason,'')<>'' OR COALESCE(c.pause_reason,'')<>'')) AS shared_paused
 FROM core.admin_messages m JOIN core.admin_message_recipients r ON r.message_id=m.id
 LEFT JOIN core.admin_message_deliveries d ON d.message_id=r.message_id AND d.destination=r.destination
 LEFT JOIN core.delivery_queue q ON q.bot_id=d.bot_id AND q.owner_kind='admin'
 AND q.owner_key=d.id::text AND q.effect_key='send'
 LEFT JOIN core.delivery_pacing b ON b.bot_id=q.bot_id AND b.chat=''
 LEFT JOIN core.delivery_pacing c ON c.bot_id=q.bot_id AND c.chat=q.chat
 WHERE m.id=$1
)
SELECT count(*)::bigint,
 count(*) FILTER (WHERE state='not_queued')::bigint,
 count(*) FILTER (WHERE state='pending')::bigint,
 count(*) FILTER (WHERE state='deferred')::bigint,
 count(*) FILTER (WHERE state='sending')::bigint,
 count(*) FILTER (WHERE state='sent')::bigint,
 count(*) FILTER (WHERE state='failed')::bigint,
 count(*) FILTER (WHERE state='cancelled')::bigint,
 count(*) FILTER (WHERE state='unknown')::bigint,
 count(*) FILTER (WHERE state='parked')::bigint,
 count(*) FILTER (WHERE state='paused')::bigint,
 count(*) FILTER (WHERE shared_paused)::bigint,
 count(*) FILTER (WHERE state NOT IN
 ('not_queued','pending','deferred','sending','sent','failed','cancelled','unknown','parked','paused'))::bigint
FROM outcomes`
