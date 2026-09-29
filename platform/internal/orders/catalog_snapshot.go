package orders

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CatalogSnapshot identifies the exact catalog used for a host-owned draft.
// Execute compares it while holding the event lock before a new mutation.
func CatalogSnapshot(event Event) string {
	data, _ := json.Marshal(event)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
