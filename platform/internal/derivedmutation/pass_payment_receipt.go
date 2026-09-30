package derivedmutation

import (
	"context"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) PassPaymentReceipt(
	ctx context.Context,
	actor string,
	command passbooking.Command,
	source readsource.Derivation,
) (passbooking.PaymentCompletionReceipt, error) {
	if !source.Valid() {
		return passbooking.PaymentCompletionReceipt{}, invalidSource()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return passbooking.PaymentCompletionReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	receipt, err := s.Registration.PaymentCompletionReceipt(ctx, tx, actor, command)
	if err != nil || !receipt.Found {
		return receipt, err
	}
	if !paymentReceiptSource(actor, source, receipt.Before) {
		return passbooking.PaymentCompletionReceipt{}, invalidSource()
	}
	return receipt, nil
}

func paymentReceiptSource(actor string, source readsource.Derivation, before passbooking.ReadAuthority) bool {
	matches := func(ref readsource.Authority) bool {
		previous := ref.Registration
		if !previous.CreatedAt.Equal(before.CreatedAt) {
			return false
		}
		previous.CreatedAt = before.CreatedAt
		return previous == before
	}
	for _, ref := range source.Authorities {
		if ref.Causal == nil {
			if matches(ref) {
				return true
			}
			continue
		}
		causal := ref.Causal
		if causal.Actor != actor || causal.Published || causal.Generation == nil ||
			*causal.Generation != *source.Generation {
			continue
		}
		if slices.ContainsFunc(causal.Authorities, matches) {
			return true
		}
	}
	return false
}
