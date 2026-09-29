package browserauth

import (
	"net/http"
	"path"
	"strings"
)

// GuardRouting rejects structural path cleanup before muxes can redirect outside
// a stripped public mount. Encoded data within path segments remains unchanged.
func GuardRouting(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authPath := strings.HasPrefix(r.URL.Path, "/miniapp/auth/") && path.Clean(r.URL.Path) != r.URL.Path
		escaped := r.URL.EscapedPath()
		canonical := path.Clean(escaped)
		if canonical != "/" && strings.HasSuffix(escaped, "/") {
			canonical += "/"
		}
		// Stripping a decoded prefix can leave an encoded separator at the start.
		malformed := !strings.HasPrefix(escaped, "/") || escaped != canonical
		if authPath || malformed {
			failure(w, http.StatusNotFound, "not_found")
			return
		}
		next.ServeHTTP(w, r)
	})
}
