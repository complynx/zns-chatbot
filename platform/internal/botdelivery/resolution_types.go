package botdelivery

import (
	"encoding/hex"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// PreparedAttempt contains host-produced correlation, never a wire body.
type PreparedAttempt struct {
	Method       string       `json:"method"`
	SHA256       string       `json:"sha256"`
	Continuation Continuation `json:"continuation"`
}

func (p PreparedAttempt) valid() bool {
	return (p.Method == "sendMessage" || p.Method == "editMessageText" || p.Method == "sendDocument") &&
		validHash(p.SHA256)
}

func validHash(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && value == strings.ToLower(value)
}

type IntentKey struct {
	Operation string `json:"operation"`
	Effect    string `json:"effect"`
}

func (k IntentKey) valid() bool {
	return k.Operation != "" && len(k.Operation) <= 200 && k.Effect != "" && len(k.Effect) <= 100
}

// Resolution is a trusted operator attestation, not verification of an external receipt.
type Resolution struct {
	IntentKey
	Key            string `json:"key"`
	Attempt        int64  `json:"attempt"`
	Disposition    string `json:"disposition"`
	EvidenceKind   string `json:"evidence_kind"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	PayloadSHA256  string `json:"payload_sha256"`
	MessageID      int64  `json:"message_id,omitempty"`
	Joined         bool   `json:"sender_joined"`
	Quiescent      bool   `json:"sink_quiescent"`
}

// Inspection deliberately omits private reference and continuation fields.
type Inspection struct {
	IntentKey
	BotID            int64         `json:"bot_id"`
	Chat             int64         `json:"chat_id"`
	Method           string        `json:"method"`
	State            delivery.Kind `json:"state"`
	Reason           string        `json:"reason"`
	Attempt          int64         `json:"attempt"`
	Phase            string        `json:"phase"`
	Target           int64         `json:"target_message_id"`
	MessageID        int64         `json:"message_id"`
	Captured         bool          `json:"captured"`
	PayloadSHA256    string        `json:"payload_sha256,omitempty"`
	BlockedFollowers int64         `json:"blocked_followers"`
	Disposition      string        `json:"disposition"`
}

type CompletionRequest struct {
	Attempt  Intent
	Outcome  delivery.Outcome
	Receipt  Continuation
	Fallback bool
}
