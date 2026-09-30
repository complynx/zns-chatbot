package delivery

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// QueueObservation contains aggregate scheduling metadata, never references or destinations.
// Terminal outcomes and per-job progress remain in the domain owner's records.
type QueueObservation struct {
	Owner              Owner
	Class              Class
	State              Kind
	Count              int64
	UnknownAge         int64
	OldestAgeSeconds   float64
	Delayed            int64
	Paused             int64
	UnboundedDeadline  int64
	NextAttemptSeconds float64
	MaximumWaitSeconds float64
}

type observationDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// QueueObservations reads one PostgreSQL statement snapshot without locks or writes.
// The timeout includes acquiring a pool connection. No scheduling policy is changed.
func QueueObservations(ctx context.Context, db observationDatabase, botID int64) ([]QueueObservation, error) {
	if botID <= 0 {
		return nil, ErrSettings
	}
	const timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := db.Query(ctx, queueObservationSQL, botID)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	defer rows.Close()
	const maximum = 7 * 2 * 5
	observations := make([]QueueObservation, 0)
	for rows.Next() {
		var item QueueObservation
		err = rows.Scan(&item.Owner, &item.Class, &item.State, &item.Count, &item.UnknownAge,
			&item.OldestAgeSeconds, &item.Delayed, &item.Paused, &item.UnboundedDeadline,
			&item.NextAttemptSeconds, &item.MaximumWaitSeconds)
		if err != nil {
			return nil, core.DatabaseOperationContextError(ctx, err)
		}
		if len(observations) == maximum || !item.Valid() {
			return nil, ErrQueueState
		}
		observations = append(observations, item)
	}
	if err = rows.Err(); err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	return observations, nil
}

// Valid enforces the fixed metric dimensions before they reach a registry.
func (o QueueObservation) Valid() bool {
	switch o.Owner {
	case Orders, Passes, Food, Massage, Admin, Announcement, Bot:
	default:
		return false
	}
	if o.Class != Interactive && o.Class != Background {
		return false
	}
	switch o.State {
	case Deferred, Sending, Uncertain, Parked, Paused:
	case Succeeded, Rejected, Cancelled:
		return false
	default:
		return false
	}
	return o.Count >= 0 && o.UnknownAge >= 0 && o.UnknownAge <= o.Count &&
		o.Delayed >= 0 && o.Delayed <= o.Count && o.Paused >= 0 && o.Paused <= o.Count &&
		o.UnboundedDeadline >= 0 && o.UnboundedDeadline <= o.Count &&
		boundedObservationSeconds(o.OldestAgeSeconds) && boundedObservationSeconds(o.NextAttemptSeconds) &&
		boundedObservationSeconds(o.MaximumWaitSeconds)
}

const maximumObservationSeconds = 365 * 24 * 60 * 60

func boundedObservationSeconds(value float64) bool {
	return value >= 0 && value <= maximumObservationSeconds
}

const queueObservationSQL = `WITH clock AS MATERIALIZED (
 SELECT statement_timestamp() AS now
), active AS (
 SELECT q.owner_kind,q.traffic_class,q.state,q.enqueued_at,clock.now,
 GREATEST(q.not_before,COALESCE(b.not_before,'-infinity'::timestamptz),
 COALESCE(c.not_before,'-infinity'::timestamptz)) AS deadline,
 (q.state IN ('paused','parked') OR COALESCE(b.pause_reason,'')<>''
 OR COALESCE(c.pause_reason,'')<>'') AS paused
 FROM core.delivery_queue q CROSS JOIN clock
 LEFT JOIN core.delivery_pacing b ON b.bot_id=q.bot_id AND b.chat=''
 LEFT JOIN core.delivery_pacing c ON c.bot_id=q.bot_id AND c.chat=q.chat
 WHERE q.bot_id=$1 AND q.state IN ('pending','sending','unknown','parked','paused')
)
SELECT owner_kind,traffic_class,state,count(*)::bigint,
 count(*) FILTER (WHERE enqueued_at IS NULL OR NOT isfinite(enqueued_at))::bigint,
 COALESCE(LEAST(31536000,GREATEST(0,EXTRACT(EPOCH FROM now-min(enqueued_at)
 FILTER (WHERE isfinite(enqueued_at))))),0)::double precision,
 count(*) FILTER (WHERE state='pending' AND deadline>now)::bigint,
 count(*) FILTER (WHERE paused)::bigint,
 count(*) FILTER (WHERE deadline='infinity'::timestamptz)::bigint,
 COALESCE(LEAST(31536000,GREATEST(0,EXTRACT(EPOCH FROM min(deadline)
 FILTER (WHERE state='pending' AND NOT paused AND deadline>now AND isfinite(deadline))-now))),0)::double precision,
 COALESCE(LEAST(31536000,GREATEST(0,EXTRACT(EPOCH FROM max(deadline)
 FILTER (WHERE state='pending' AND isfinite(deadline))-now))),0)::double precision
FROM active GROUP BY owner_kind,traffic_class,state,now LIMIT 71`
