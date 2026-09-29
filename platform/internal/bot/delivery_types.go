package bot

import (
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type botRenderedDelivery struct {
	Payload      telegram.Send
	Filename     string
	Body         []byte
	Receipt      botdelivery.Continuation
	ExportEvents []string
}
