package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (b *Bot) addKnownReceipt(ctx context.Context, owner string, input *agent.Input) error {
	var latest *orders.Change
	for _, change := range input.OrderHistory {
		if change.Action == stateProof && change.State == stateProof &&
			(latest == nil || change.At.After(latest.At)) {
			copyChange := change
			latest = &copyChange
		}
	}
	if latest == nil {
		return nil
	}
	result := &agent.ReceiptSummary{OrderID: latest.OrderID, SubmittedAt: latest.At,
		SubmittedVersion: latest.Version, Origin: latest.Origin}
	current, err := b.API.Order(ctx, owner, b.currentOrderEvent(), latest.OrderID)
	if core.IsDatabaseFailure(err) {
		return err
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusNotFound {
		input.MediaContext.LatestKnownReceipt = result
		return nil
	}
	if err != nil {
		return err
	}
	result.CurrentState = current.State
	input.MediaContext.LatestKnownReceipt = result
	return nil
}
