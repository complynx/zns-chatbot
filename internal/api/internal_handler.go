package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Long reads extend socket deadlines as well as the request context. Other
// routes retain the server defaults.
func (s *Server) serveInternal(w http.ResponseWriter, r *http.Request) {
	timeout := internalReadTimeout(r)
	if timeout == 0 {
		s.internal.ServeHTTP(w, r)
		return
	}
	serveBoundedAPI(w, r, s.internal, timeout)
}

func serveBoundedAPI(w http.ResponseWriter, r *http.Request, handler http.Handler, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	controller := http.NewResponseController(w)
	if controller.SetReadDeadline(deadline) != nil || controller.SetWriteDeadline(deadline.Add(time.Second)) != nil {
		http.Error(w, "bounded request IO unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	handler.ServeHTTP(w, r.WithContext(ctx))
}

func (s *Server) servePublic(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/app/avatars/history/") &&
		strings.HasSuffix(r.URL.Path, "/preview") {
		serveBoundedAPI(w, r, s.public, historyAvatarReadTimeout)
		return
	}
	source := strings.HasPrefix(r.URL.Path, "/app/avatars/drafts/") &&
		((r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/original") || strings.Contains(r.URL.Path, "/preview/"))) ||
			(r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/finalize")))
	if source ||
		(r.Method == http.MethodPost && (r.URL.Path == "/app/avatars/drafts" || r.URL.Path == "/app/avatars/drafts/upload")) {
		serveBoundedAPI(w, r, s.public, avatarUploadTimeout)
		return
	}
	s.public.ServeHTTP(w, r)
}

func internalReadTimeout(r *http.Request) time.Duration {
	if (r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/internal/avatar-drafts/")) ||
		(r.Method == http.MethodPost && (r.URL.Path == avatarPath+"create_draft" || r.URL.Path == avatarPath+avatarRefetchMethod || r.URL.Path == avatarPath+"preview_draft" || r.URL.Path == avatarPath+"finalize_draft")) {
		return avatarUploadTimeout
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/internal/history-avatars/") &&
		strings.HasSuffix(r.URL.Path, "/content") {
		return historyAvatarReadTimeout
	}
	if r.Method != http.MethodPost {
		return 0
	}
	switch r.URL.Path {
	case historyPath + historyAvatarFetchMethod, historyPath + historyAvatarRefetchMethod:
		return historyAvatarReadTimeout
	case historyPath + "avatar_info", historyRegistrationPath, historyRegistrationsPath,
		"/internal/admin/legacy/validate", "/internal/agent/administration/validate_stored_archive",
		"/internal/agent/administration/validate_archive", "/internal/admin/legacy/store",
		"/internal/agent/administration/apply_history", "/internal/agent/administration/store_archive":
		return archiveValidationTimeout
	default:
		return 0
	}
}
