package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestStaticWritesHaveRecoveryContracts(t *testing.T) {
	t.Parallel()
	const minimumDedupSeconds = 86400
	w := httptest.NewRecorder()
	(&Server{}).manifest(w, nil)
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID           string `json:"id"`
				Effect       string `json:"effect"`
				StatusPath   string `json:"status_path"`
				Idempotency  bool   `json:"idempotency"`
				DedupSeconds int    `json:"deduplication_window_seconds"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &manifest))
	foundPrepare := false
	for _, module := range manifest.Modules {
		for _, method := range module.Methods {
			if method.Effect == effectRead {
				continue
			}
			label := module.ID + "." + method.ID
			require.True(t, method.Idempotency, label)
			require.NotEmpty(t, method.StatusPath, label)
			require.GreaterOrEqual(t, method.DedupSeconds, minimumDedupSeconds, label)
			if module.ID == "zinusya_communications" && method.ID == communicationPrepareMethod {
				foundPrepare = true
				require.Equal(t, communicationPath+"prepare/status", method.StatusPath)
			}
		}
	}
	require.True(t, foundPrepare, "communications.prepare must be published as a write")
}
