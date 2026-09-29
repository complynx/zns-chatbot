package migrate

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func deferPassFood(ctx context.Context, tx pgx.Tx, row PassPlanRecord) error {
	if row.Source != passesSource {
		return nil
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Record, &fields)
	markers := map[string]json.RawMessage{}
	for _, field := range []string{"notified_food_first", "notified_food_last"} {
		if value, exists := fields[field]; exists {
			markers[field] = value
		}
	}
	if len(markers) == 0 {
		return nil
	}
	raw, _ := json.Marshal(markers)
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_pass_deferred_domains(source_key,domain,source_record) VALUES($1,'food',$2)`,
		row.Legacy.Key,
		raw,
	)
	if err != nil {
		return errors.New("apply_deferred_domain_conflict")
	}
	return nil
}

func countPassDeferred(ctx context.Context, tx pgx.Tx, p preparedPasses) (int, error) {
	keys := make([]string, 0, len(p.Users))
	for _, user := range p.Users {
		keys = append(keys, user.Legacy.Key)
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.legacy_user_deferred_domains WHERE source_key=ANY($1::text[]) AND NOT completed)+(SELECT count(*) FROM core.legacy_pass_deferred_domains d JOIN core.legacy_pass_import_references r USING(source_key) WHERE r.bot_id=$2 AND NOT d.completed)`, keys, p.Plan.BotID).
		Scan(&count)
	if err != nil {
		return 0, errors.New("apply_deferred_domain_unresolved")
	}
	return count, nil
}

func insertPassAssignmentMetadata(ctx context.Context, tx pgx.Tx, row PassPlanRecord, owners map[string]string) error {
	b := row.Candidate
	if b.Assigned == nil {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_pass_assignment_metadata(event_id,owner,assigned_at,source_key,assignment_tier_number) VALUES($1,$2,$3,$4,$5)`,
		b.Event,
		passOwner(owners, b.TelegramID),
		b.Assigned,
		row.Legacy.Key,
		b.AssignmentTier,
	)
	if err != nil {
		return errors.New("apply_assignment_metadata_conflict")
	}
	return nil
}
