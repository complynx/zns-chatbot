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
	individualPrices bool,
) ([]registrations.PriceQuote, error) {
	if event.Document.Registration == nil {
		return nil, registrations.ErrUnavailable
	}
	rules := *event.Document.Registration
	var usage []registrations.TierUsage
	if rules.Pricing != nil {
		var err error
		usage, err = tierUsage(ctx, q, event.ID)
		if err != nil {
			return nil, err
		}
	}
	return registrations.ResolveAssignmentPrices(rules, now, usage, roles, individualPrices)
}

// tierUsage includes each assigned pass or completed promo entitlement exactly once.
func tierUsage(ctx context.Context, q *sqlc.Queries, eventID uuid.UUID) ([]registrations.TierUsage, error) {
	rows, err := q.ListTierUsage(ctx, eventID)
	if err != nil {
		return nil, err
	}
	usage := make([]registrations.TierUsage, 0, len(rows))
	for _, row := range rows {
		usage = append(usage, registrations.TierUsage{Role: row.Role, TierID: row.TierID, Count: row.Count})
	}
	return usage, nil
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
