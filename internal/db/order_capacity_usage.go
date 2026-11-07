package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// checkOrderResourceCeilings uses the caller's event lock. Configuration cannot
// remove or lower a pool below held plus occupied units, including old orders.
func checkOrderResourceCeilings(
	ctx context.Context,
	q *sqlc.Queries,
	event uuid.UUID,
	policy *orders.CompleteCapacity,
) error {
	usage, err := q.OrderResourceUsage(ctx, event)
	if err != nil {
		return err
	}
	ceilings := make(map[string]int64)
	if policy != nil {
		for _, resource := range policy.Resources {
			ceilings[resource.ID] = resource.Ceiling
		}
	}
	for _, used := range usage {
		ceiling, exists := ceilings[used.ResourceID]
		if !exists || ceiling < used.Units {
			return ErrConflict
		}
	}
	return nil
}

func orderCapacityShortage(
	ctx context.Context,
	q *sqlc.Queries,
	event uuid.UUID,
	snapshot *orders.CapacitySnapshot,
) (bool, error) {
	if snapshot == nil || snapshot.Version != 1 || snapshot.Demands == nil ||
		len(snapshot.Demands) > orders.MaxResources {
		return false, ErrConflict
	}
	if len(snapshot.Demands) == 0 {
		return false, nil
	}
	row, err := q.ReadOrdersCatalog(ctx, event)
	if err != nil {
		return false, err
	}
	catalog, err := ordersCatalogRecord(row)
	if err != nil {
		return false, err
	}
	policy := catalog.Document.CompleteCapacity
	if policy == nil {
		return false, ErrConflict
	}
	usage, err := q.OrderResourceUsage(ctx, event)
	if err != nil {
		return false, err
	}
	occupied := make(map[string]int64, len(usage))
	for _, used := range usage {
		occupied[used.ResourceID] = used.Units
	}
	ceilings := make(map[string]int64, len(policy.Resources))
	for _, resource := range policy.Resources {
		ceilings[resource.ID] = resource.Ceiling
	}
	seen := make(map[string]bool, len(snapshot.Demands))
	shortage := false
	for _, demand := range snapshot.Demands {
		ceiling, exists := ceilings[demand.ResourceID]
		if !exists || seen[demand.ResourceID] || demand.Units < 1 || demand.Units > orders.MaxResourceUnits {
			return false, ErrConflict
		}
		seen[demand.ResourceID] = true
		if demand.Units > ceiling-occupied[demand.ResourceID] {
			shortage = true
		}
	}
	return shortage, nil
}
