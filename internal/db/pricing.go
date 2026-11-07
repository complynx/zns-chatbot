package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// assignmentPrices includes every assigned owner pass, including unpaid and legacy passes.
// Callers hold the event lock and supply the final database instant for assignment.
func assignmentPrices(
	ctx context.Context,
	q *sqlc.Queries,
	event events.Event,
	now time.Time,
	roles []string,
) ([]registrations.PriceQuote, error) {
	if event.Document.Registration == nil {
		return nil, registrations.ErrUnavailable
	}
	rules := *event.Document.Registration
	var usage []registrations.TierUsage
	if rules.Pricing != nil {
		rows, err := q.ListTierUsage(ctx, event.ID)
		if err != nil {
			return nil, err
		}
		usage = make([]registrations.TierUsage, 0, len(rows))
		for _, row := range rows {
			usage = append(usage, registrations.TierUsage{Role: row.Role, TierID: row.TierID, Count: row.Count})
		}
	}
	return registrations.ResolvePrices(rules, now, usage, roles)
}

func writePassTier(ctx context.Context, q *sqlc.Queries, passID uuid.UUID, quote registrations.PriceQuote) error {
	if quote.Tier == nil {
		return nil
	}
	return q.CreatePassTier(
		ctx,
		sqlc.CreatePassTierParams{PassID: passID, TierID: quote.Tier.ID, TierNumber: quote.Tier.Number},
	)
}

func readPassTier(ctx context.Context, q *sqlc.Queries, passID uuid.UUID) (*registrations.TierSnapshot, error) {
	row, err := q.GetPassTier(ctx, passID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // Flat owner passes have no tier snapshot.
	}
	if err != nil {
		return nil, err
	}
	return &registrations.TierSnapshot{ID: row.TierID, Number: row.TierNumber}, nil
}
