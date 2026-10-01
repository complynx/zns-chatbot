package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Hash the normalized logical request, including its method and exact edit target.
// Documents use their content hash, not a random multipart boundary or retained body.
func prepareBotAttempt(i botdelivery.Intent, r botRenderedDelivery) (botdelivery.PreparedAttempt, error) {
	method := "sendMessage"
	originalTarget := r.Payload.MessageID
	r.Payload.ChatID, r.Payload.MessageID = i.Chat, 0
	if i.Phase == botDocumentKind {
		method = "sendDocument"
	}
	if i.Phase == botPhaseEdit {
		method = "editMessageText"
	}
	target := i.Target
	// Reconstruction supplies the first admission's actual edit target.
	if i.Attempt == 0 && i.Reference.Kind == botdelivery.CardIntent {
		if originalTarget > 0 {
			target = originalTarget
		}
		if target > 0 {
			method = "editMessageText"
		}
	}
	if method == "editMessageText" {
		r.Payload.MessageID = target
	}
	wire := struct {
		Method         string         `json:"method"`
		Payload        *telegram.Send `json:"payload,omitempty"`
		Chat           int64          `json:"chat_id,omitempty"`
		Filename       string         `json:"filename,omitempty"`
		DocumentSHA256 string         `json:"document_sha256,omitempty"`
	}{Method: method, Payload: &r.Payload}
	if method == "sendDocument" {
		body := sha256.Sum256(r.Body)
		wire.Payload, wire.Chat, wire.Filename = nil, i.Chat, r.Filename
		wire.DocumentSHA256 = hex.EncodeToString(body[:])
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return botdelivery.PreparedAttempt{}, err
	}
	hash := sha256.Sum256(raw)
	return botdelivery.PreparedAttempt{
		Method:       method,
		SHA256:       hex.EncodeToString(hash[:]),
		Continuation: r.Receipt,
	}, nil
}
