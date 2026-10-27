package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileSourceRefusalPreservesOwnerAuthorityMetadataAndRecovery(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileBytes = []byte("%PDF-1.4\nsource recovery")
	h := setup(t, p)
	_, owner := registrationUser(t, h, 456, false)
	_, foreign := registrationUser(t, h, 9952, false)
	id := sizedFileID(t, h, 984, 0)
	metadata := fileInfo(t, h, owner, id)
	server := httptest.NewServer(h.endpoint.InternalHandler())
	t.Cleanup(server.Close)
	read := func(token string, expected int, detail string) {
		t.Helper()
		code, data := adminRequest(t, h, token, http.MethodPost, "/internal/agent/files/fetch", map[string]any{
			"protocol_version": 1, "operation_id": "source-read", "arguments": map[string]any{"external_id": id},
		})
		require.Equal(t, expected, code, string(data))
		require.Contains(t, string(data), detail)
		require.NotContains(t, string(data), "PRIVATE")
		require.NotContains(t, string(data), "test-token")
		req, err := http.NewRequest(http.MethodGet, server.URL+"/internal/files/"+id.String()+"/content", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		require.NoError(t, err)
		require.Equal(t, expected, response.StatusCode)
		require.NotContains(t, string(body), "PRIVATE")
		require.NotContains(t, string(body), "test-token")
		if expected == http.StatusOK {
			require.Equal(t, p.fileBytes, body)
		}
	}
	for _, tc := range []struct {
		code, expected int
		reason, detail string
	}{
		{400, 404, "Bad Request: invalid file_id", "file_unavailable"},
		{403, 424, "Forbidden: PRIVATE", "file_source_access_denied"},
		{400, 503, "Bad Request: wrong file_id or the file is temporarily unavailable", "Telegram file unavailable"},
		{0, 200, "", "sha256"},
	} {
		p.mu.Lock()
		p.fileErrorCode, p.fileErrorText = tc.code, tc.reason
		before := p.fileGets
		p.mu.Unlock()
		read(foreign, http.StatusNotFound, "file unavailable")
		p.mu.Lock()
		foreignGets := p.fileGets
		p.mu.Unlock()
		require.Equal(t, before, foreignGets, "foreign reads must not contact Telegram")
		read(owner, tc.expected, tc.detail)
		require.Equal(t, metadata, fileInfo(t, h, owner, id),
			"source errors must preserve metadata and caller authority")
	}
}
