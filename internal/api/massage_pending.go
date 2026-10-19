package api

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) ownPendingMassageProposals(ctx context.Context,
	in *agentReadRequest[massageBookingListArguments],
) (*agentReadResponse[db.MassagePendingPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.OwnPendingMassageProposals(ctx, scope, a.EventID, a.After, a.Limit)
	if err == nil {
		result, err = boundedMassagePendingPage(result)
	}
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

// Keep both native and agent encoded pages within the existing massage payload budget.
func boundedMassagePendingPage(page db.MassagePendingPage) (db.MassagePendingPage, error) {
	for {
		raw, err := json.Marshal(page)
		if err != nil {
			return page, err
		}
		if len(raw) <= 60*1024 {
			return page, nil
		}
		if len(page.Proposals) <= 1 {
			return db.MassagePendingPage{}, db.ErrConflict
		}
		page.Proposals = page.Proposals[:len(page.Proposals)-1]
		next := page.Proposals[len(page.Proposals)-1].ID
		page.NextAfter = &next
	}
}
