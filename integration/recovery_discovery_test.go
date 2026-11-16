package integration_test

import (
	"net/http"

	"github.com/complynx/zns-chatbot/internal/loom"
)

// respondRecoveryDiscovery serves the shared v1 discovery surface. The caller
// supplies its scenario's locator state and owns any peer lock.
func respondRecoveryDiscovery(w http.ResponseWriter, r *http.Request, located loom.ActiveRun) bool {
	switch r.URL.Path {
	case "/v1/capabilities":
		respondRecoveryCapabilities(w)
	case "/v1/chats/default":
		respond(w, map[string]string{"id": "retained-chat"})
	case "/v1/chats/retained-chat/active-run":
		respond(w, located)
	default:
		return false
	}
	return true
}
