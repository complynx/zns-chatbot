package knowledge

import "encoding/json"

// MemoryResponseBytes leaves room for the host's response envelope under its
// 32 KiB callback limit. Measure JSON bytes, including escaping, not text runes.
const MemoryResponseBytes = 24 * 1024

func memoryFits(value any) bool {
	if page, ok := value.(MemoryPage); ok {
		page.Scanned = memoryScanLimit
		page.More = false
		page.Incomplete = false
		value = page
	}
	data, err := json.Marshal(value)
	return err == nil && len(data) <= MemoryResponseBytes
}
