package bot

import (
	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// Keep both domains discoverable when an owner has many unpaid orders. Matching
// still uses every authorized quote, not this bounded button projection.
func visibleReceiptCandidates(candidates []agent.MediaCandidate) []agent.MediaCandidate {
	const perDomain = 20
	orders, registrations := 0, 0
	var visible []agent.MediaCandidate
	for _, candidate := range candidates {
		if candidate.RegistrationEvent != "" {
			if registrations >= perDomain {
				continue
			}
			registrations++
		} else {
			if orders >= perDomain {
				continue
			}
			orders++
		}
		visible = append(visible, candidate)
	}
	return visible
}
