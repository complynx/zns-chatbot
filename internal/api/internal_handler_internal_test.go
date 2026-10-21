package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type deadlineResponse struct {
	*httptest.ResponseRecorder

	read  time.Time
	write time.Time
}

func (w *deadlineResponse) SetReadDeadline(deadline time.Time) error  { w.read = deadline; return nil }
func (w *deadlineResponse) SetWriteDeadline(deadline time.Time) error { w.write = deadline; return nil }

func TestHistoryImageRequestsHaveTimeToReturnAfterBoundedReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method, path string
		budget       time.Duration
	}{
		{http.MethodPost, historyPath + "avatar_info", archiveValidationTimeout},
		{http.MethodPost, historyPath + historyAvatarFetchMethod, historyAvatarReadTimeout},
		{http.MethodPost, historyPath + historyAvatarRefetchMethod, historyAvatarReadTimeout},
		{http.MethodPost, avatarPath + avatarRefetchMethod, avatarUploadTimeout},
		{http.MethodGet, "/internal/history-avatars/archive/record/content", historyAvatarReadTimeout},
		{http.MethodGet, "/internal/agent/manifest", 0},
		{http.MethodPost, avatarPath + "create_draft", avatarUploadTimeout},
		{http.MethodPost, avatarPath + "preview_draft", avatarUploadTimeout},
		{http.MethodPost, avatarPath + "finalize_draft", avatarUploadTimeout},
		{http.MethodGet, "/internal/avatar-drafts/draft/original", avatarUploadTimeout},
		{http.MethodPost, "/app/avatars/drafts", avatarUploadTimeout},
		{http.MethodGet, "/app/avatars/drafts/draft/original", avatarUploadTimeout},
		{http.MethodGet, "/app/avatars/history/archive/record/preview", historyAvatarReadTimeout},
		{http.MethodGet, "/app/avatars/history", 0},
		{http.MethodPost, "/app/avatars/history/archive/record/preview", 0},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			writer := &deadlineResponse{ResponseRecorder: httptest.NewRecorder()}
			started := time.Now()
			var deadline time.Time
			var exists bool
			mux.HandleFunc(tc.path, func(w http.ResponseWriter, r *http.Request) {
				deadline, exists = r.Context().Deadline()
				w.WriteHeader(http.StatusNoContent)
			})
			server := &Server{internal: mux, public: mux}
			request := httptest.NewRequest(tc.method, tc.path, nil)
			if strings.HasPrefix(tc.path, "/app/") {
				server.servePublic(writer, request)
			} else {
				server.serveInternal(writer, request)
			}
			require.Equal(t, http.StatusNoContent, writer.Code)
			if tc.budget == 0 {
				require.False(t, exists)
				require.True(t, writer.read.IsZero())
				require.True(t, writer.write.IsZero())
			} else {
				require.True(t, exists)
				require.WithinDuration(t, started.Add(tc.budget), deadline, time.Second)
				require.Equal(t, deadline, writer.read)
				require.Equal(t, deadline.Add(time.Second), writer.write)
			}
		})
	}
}
