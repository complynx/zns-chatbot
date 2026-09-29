package migrate

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func insertEventConfiguration(ctx context.Context, tx pgx.Tx, event *EventCandidate) error {
	c := event.Configuration
	titles, err := json.Marshal(c.ShortTitles)
	if err != nil {
		return errors.New("event_title_invalid")
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.pass_events SET short_titles=$2,country_emoji=$3,thread_channel=$4,thread_id=$5,thread_locale=$6,default_price=$7,amount_cap_per_role=$8,open_ended=$9 WHERE id=$1`,
		event.ID,
		titles,
		c.CountryEmoji,
		c.ThreadChannel,
		c.ThreadID,
		c.ThreadLocale,
		c.DefaultPrice,
		c.AmountCapPerRole,
		c.OpenEnded,
	)
	if err != nil {
		return errors.New("apply_event_configuration_conflict")
	}
	return nil
}

func reconcileEventConfiguration(ctx context.Context, tx pgx.Tx, event *EventCandidate) error {
	c := event.Configuration
	titles, err := json.Marshal(c.ShortTitles)
	if err != nil {
		return errors.New("event_title_invalid")
	}
	var matched bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_events WHERE id=$1 AND short_titles=$2::jsonb AND country_emoji=$3 AND thread_channel=$4 AND thread_id IS NOT DISTINCT FROM $5::bigint AND thread_locale=$6 AND default_price IS NOT DISTINCT FROM $7::integer AND amount_cap_per_role=$8 AND open_ended=$9)`, event.ID, titles, c.CountryEmoji, c.ThreadChannel, c.ThreadID, c.ThreadLocale, c.DefaultPrice, c.AmountCapPerRole, c.OpenEnded).
		Scan(&matched)
	if err != nil || !matched {
		return errors.New("apply_reconciliation_failed")
	}
	return nil
}
