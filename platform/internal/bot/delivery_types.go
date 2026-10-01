package bot

import (
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Provider evidence is retained before edit policy changes the delivery result.
type botTransportResult struct {
	Outcome         delivery.Outcome
	ProviderOutcome delivery.Outcome
	Fallback        bool
}

type botRenderedDelivery struct {
	Wire         *botdelivery.WireReference
	Payload      telegram.Send
	Filename     string
	Body         []byte
	Receipt      botdelivery.Continuation
	ExportEvents []string
}

func (r botRenderedDelivery) privateWire() botdelivery.Wire {
	return botdelivery.Wire{Payload: r.Payload, Filename: r.Filename, Body: r.Body,
		Receipt: r.Receipt, ExportEvents: r.ExportEvents}
}

func renderedBotWire(wire botdelivery.Wire, ref *botdelivery.WireReference) botRenderedDelivery {
	return botRenderedDelivery{Payload: wire.Payload, Filename: wire.Filename, Body: wire.Body,
		Receipt: wire.Receipt, ExportEvents: wire.ExportEvents, Wire: ref}
}
