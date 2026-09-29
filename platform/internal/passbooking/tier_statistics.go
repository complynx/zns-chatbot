package passbooking

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Tier reads retain statistics only. History, identities and comments never enter Go memory.
func readTierStatistics(ctx context.Context, tx pgx.Tx, eventID string) (statistics, error) {
	result := newStatistics()
	rows, err := tx.Query(ctx, `SELECT state,role,price,tier_index,skip_balance FROM core.pass_bookings
 WHERE event_id=$1 AND state IN ('assigned','paid','waitlist')`, eventID)
	if err != nil {
		return statistics{}, err
	}
	defer rows.Close()
	usageBytes := 2
	for rows.Next() {
		var booking Booking
		if err = rows.Scan(
			&booking.State,
			&booking.Role,
			&booking.Price,
			&booking.TierIndex,
			&booking.SkipBalance,
		); err != nil {
			return statistics{}, err
		}
		usageBytes = nextUsageBytes(usageBytes, result, &booking)
		// Each role map is a subset of this output map, so none can grow independently.
		if usageBytes > core.ReadResourceBytes {
			return statistics{}, core.ReadProblem("read_result_limit")
		}
		result.observe(&booking)
	}
	if err = rows.Err(); err != nil {
		return statistics{}, err
	}
	return result, nil
}

func nextUsageBytes(size int, stats statistics, b *Booking) int {
	if (b.State != assigned && b.State != paid) || b.TierIndex == nil {
		return size
	}
	previous := stats.total.Explicit[*b.TierIndex]
	if previous != 0 {
		return size + len(strconv.Itoa(previous+1)) - len(strconv.Itoa(previous))
	}
	// An entry contributes the quoted decimal key, colon, value and comma.
	const entryFramingBytes = 5
	size += len(strconv.Itoa(*b.TierIndex)) + entryFramingBytes
	if len(stats.total.Explicit) == 0 {
		size--
	}
	return size
}
