package agenthost

import (
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
)

// CurrentRequestEvidence treats Telegram voice as a direct spoken request. Speech in uploaded recordings is
// evidence for interpretation, not automatic authority to select an order.
// This only supplies selection evidence; normal owner/version checks still apply.
func CurrentRequestEvidence(input agent.Input) string {
	if input.AV != nil && input.AV.Kind == string(mediaclient.Voice) && input.AV.Transcript.Status == "ok" {
		return input.Text + "\n" + input.AV.Transcript.Text
	}
	return input.Text
}
