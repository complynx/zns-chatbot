package task

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/observability"
)

// QueueMetrics reads River's own schema without reading job payloads. Future
// snoozes are excluded; due scheduled/retryable jobs still need promotion.
func QueueMetrics(ctx context.Context, pool *pgxpool.Pool) ([]observability.QueueMetric, error) {
	schema, err := queueSchema(pool)
	if err != nil {
		return nil, err
	}
	table := pgx.Identifier{schema, "river_job"}.Sanitize()
	rows, err := pool.Query(
		ctx,
		`SELECT kind,state::text,count(*)::bigint,
        extract(epoch FROM statement_timestamp()-min(scheduled_at))::double precision
        FROM `+table+` WHERE queue=$1 AND kind=ANY($2::text[])
        AND state IN ('available','scheduled','retryable') AND scheduled_at<=statement_timestamp()
        GROUP BY kind,state`,
		river.QueueDefault,
		[]string{
			(JobArgs{}).Kind(),
			(notificationArgs{}).Kind(),
			(pairNotificationArgs{}).Kind(),
			(orderNotificationArgs{}).Kind(),
			(avatarDeliveryArgs{}).Kind(),
			(massageReminderArgs{}).Kind(),
		},
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []observability.QueueMetric
	for rows.Next() {
		var row observability.QueueMetric
		if err = rows.Scan(&row.Kind, &row.State, &row.Count, &row.ScheduledAgeSeconds); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
