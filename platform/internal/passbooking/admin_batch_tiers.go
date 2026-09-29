package passbooking

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

type TierStatus struct {
	FullBalance  passallocation.Counts      `json:"full_balance"`
	Target       passallocation.Role        `json:"target"`
	Distributed  passallocation.TierReport  `json:"distributed"`
	Leader       passallocation.TierReport  `json:"leader"`
	Follower     passallocation.TierReport  `json:"follower"`
	Couple       *passallocation.TierDetail `json:"couple,omitempty"`
	Event        string                     `json:"event"`
	Rule         passallocation.Rule        `json:"rule"`
	Balance      passallocation.Counts      `json:"balance"`
	Waiting      passallocation.Counts      `json:"waiting"`
	Participants passallocation.Counts      `json:"participants"`
	Assigned     passallocation.Counts      `json:"assigned"`
	Current      int                        `json:"current"`
	Tiers        []passallocation.Tier      `json:"tiers"`
	Usage        map[int]int                `json:"usage"`
}

// TierStatus shares the allocator's snapshot, including explicit balance exclusions.
func (s Service) TierStatus(ctx context.Context, actor, eventID string) (TierStatus, error) {
	var result TierStatus
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	e, err := readEvent(ctx, tx, eventID)
	if err != nil {
		return result, err
	}
	if _, err = authorize(ctx, tx, actor, commandAdminCancel, eventID); err != nil {
		return result, err
	}
	records, err := readBookings(ctx, tx, eventID)
	if err != nil {
		return result, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, err
	}
	state := newSnapshot(e, records, now)
	result = state.tierStatus()
	return result, tx.Commit(ctx)
}
