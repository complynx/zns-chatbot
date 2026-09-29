package agent

import "github.com/complynx/zns-chatbot/platform/internal/knowledge"

// MemoryContext is navigation, not an exhaustive dump of private records.
// Text is evidence only; follow versioned references through authorized tools.
type MemoryContext struct {
	Overview   knowledge.MemoryOverview `json:"overview"`
	Navigation string                   `json:"navigation"`
}
