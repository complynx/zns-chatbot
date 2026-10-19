package api

import (
	"context"
	"net/http"
	"time"
)

// Archive reads need bounded socket IO as well as a context, including time to
// return their result after validation. Other routes keep the server defaults.
func (s *Server) serveInternal(w http.ResponseWriter, r *http.Request) {
	archive := r.URL.Path == "/internal/admin/legacy/validate" ||
		r.URL.Path == historyRegistrationPath ||
		r.URL.Path == historyRegistrationsPath ||
		r.URL.Path == "/internal/agent/administration/validate_stored_archive" ||
		r.URL.Path == "/internal/agent/administration/validate_archive" ||
		r.URL.Path == "/internal/admin/legacy/store" ||
		r.URL.Path == "/internal/agent/administration/apply_history" ||
		r.URL.Path == "/internal/agent/administration/store_archive"
	if r.Method != http.MethodPost || !archive {
		s.internal.ServeHTTP(w, r)
		return
	}
	deadline := time.Now().Add(archiveValidationTimeout)
	controller := http.NewResponseController(w)
	if controller.SetReadDeadline(deadline) != nil || controller.SetWriteDeadline(deadline.Add(time.Second)) != nil {
		http.Error(w, "bounded archive IO unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	s.internal.ServeHTTP(w, r.WithContext(ctx))
}
