package agent

// AssetContext contains observations of shared artwork, not user instructions.
// Occurrences use Telegram UTF-16 offsets in the original text or caption.
type AssetContext struct {
	Items   []AssetObservation `json:"items"`
	Omitted int                `json:"omitted,omitempty"`
}

type AssetObservation struct {
	Kind        string            `json:"kind"`
	Status      string            `json:"status"`
	Description string            `json:"description,omitempty"`
	Occurrences []AssetOccurrence `json:"occurrences,omitempty"`
}

type AssetOccurrence struct {
	Offset      int    `json:"offset"`
	Length      int    `json:"length"`
	Placeholder string `json:"placeholder"`
}
