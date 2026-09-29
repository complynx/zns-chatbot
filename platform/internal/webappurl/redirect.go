package webappurl

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/felixge/httpsnoop"
)

// SubtreeRedirects keeps ServeMux slash redirects inside the configured public
// mount. Other redirects retain the destination selected by their handler.
func SubtreeRedirects(publicURL string, next http.Handler) http.Handler {
	mount, err := Route(publicURL, "/")
	if err != nil {
		return next
	}
	prefix := strings.TrimSuffix(mount.EscapedPath(), "/")
	if prefix == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.EscapedPath() + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		destination := url.URL{
			Path:     strings.TrimSuffix(mount.Path, "/") + r.URL.Path + "/",
			RawPath:  prefix + r.URL.EscapedPath() + "/",
			RawQuery: r.URL.RawQuery,
		}
		replaced := false
		writer := httpsnoop.Wrap(w, httpsnoop.Hooks{
			WriteHeader: func(writeHeader httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				return func(status int) {
					if status == http.StatusTemporaryRedirect && w.Header().Get("Location") == target {
						replaced = true
						// Generate both Location and the fallback HTML link from the public URL.
						w.Header().Del("Content-Type")
						http.Redirect(w, r, destination.String(), status)
						return
					}
					writeHeader(status)
				}
			},
			Write: func(write httpsnoop.WriteFunc) httpsnoop.WriteFunc {
				return func(body []byte) (int, error) {
					if replaced {
						return len(body), nil
					}
					return write(body)
				}
			},
		})
		next.ServeHTTP(writer, r)
	})
}
