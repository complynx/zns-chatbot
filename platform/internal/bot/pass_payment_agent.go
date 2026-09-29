package bot

import (
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func registrationReviewEvidence(context *agent.RegistrationContext, p agent.RegistrationProposal) *passbooking.Payment {
	for _, read := range slices.Backward(context.Reads) {
		if read.Request.Event != p.Event || read.Request.View != registrationPaymentQueue || read.Error != "" {
			continue
		}
		for _, item := range read.PaymentQueue {
			if item.Owner == p.Target && item.Payment.Decision == "pending" {
				return &item.Payment
			}
		}
	}
	return nil
}
