package identity

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

// BrowserSession signs only an opaque consent record, never user privileges.
func (s Signer) BrowserSession(request string, expires time.Time) string {
	return s.tokenUntil(request, "zns-browser-session", expires)
}

func (s Signer) VerifyBrowserSession(token string) (string, error) {
	return s.verify(token, "zns-browser-session")
}

// BrowserOrigins parses an opt-in space-separated exact HTTPS origin allowlist.
func BrowserOrigins(raw string) (map[string]bool, error) {
	origins := map[string]bool{}
	for origin := range strings.FieldsSeq(raw) {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != httpsScheme || u.Host == "" || strings.Contains(u.Host, "*") || origins[origin] ||
			u.User != nil ||
			u.Path != "" ||
			u.RawQuery != "" ||
			u.Fragment != "" ||
			origin != u.Scheme+"://"+u.Host {
			return nil, errors.New("legacy browser origins must be unique exact HTTPS origins separated by spaces")
		}
		origins[origin] = true
	}
	return origins, nil
}
