package api

import "github.com/complynx/zns-chatbot/internal/db"

// Agent envelopes carry a bounded description. The approval handler reads the
// full immutable presentation directly from the authorized operation receipt.
func orderReviewAgentWorkspace(w db.OrderReviewWorkspace) db.OrderReviewWorkspace {
	w.Preparation.Description = orderReviewPrepareDescription(db.OrderReviewCommitPreparation{
		Description: w.Preparation.Description,
		Arguments: db.OrderReviewCommitChange{
			WorkspaceID: w.ID,
			Revision:    w.Revision,
			Review:      w.Preparation.Arguments,
		},
	})
	return w
}

func orderReviewAgentReceipt(receipt db.OrderReviewReceipt) db.OrderReviewReceipt {
	if receipt.Workspace != nil {
		w := orderReviewAgentWorkspace(*receipt.Workspace)
		receipt.Workspace = &w
	}
	if receipt.Preparation != nil {
		p := *receipt.Preparation
		p.Description = orderReviewPrepareDescription(p)
		receipt.Preparation = &p
	}
	return receipt
}
