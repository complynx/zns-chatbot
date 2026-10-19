package miniapp

import (
	"net/url"
	"path"
	"strconv"
	"strings"
)

// ValidOrigin accepts one explicit HTTPS origin, without paths or credentials.
// Request Host and forwarded headers never establish the application trust root.
func ValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.String() == origin && u.Scheme == "https" && u.Host != "" && u.Hostname() != "" &&
		u.Host == strings.ToLower(u.Host) && u.Port() != "443" &&
		u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == ""
}

// ValidSessionOrigin retains HTTPS by default. The explicit development switch
// permits only canonical http://localhost origins, never remote hosts or aliases.
func ValidSessionOrigin(origin string, allowLocalHTTP bool) bool {
	if ValidOrigin(origin) {
		return true
	}
	if !allowLocalHTTP {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.String() != origin || u.Scheme != "http" || u.Hostname() != "localhost" ||
		u.Host != strings.ToLower(u.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Port() == "80" {
		return false
	}
	return canonicalPort(u)
}

// ValidLaunchURL validates the HTTPS URL Telegram opens. Its path can include a
// configured redirect prefix; it does not define the trusted session origin.
func ValidLaunchURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.String() != value || u.User != nil {
		return false
	}
	if !ValidOrigin(u.Scheme+"://"+u.Host) || !canonicalPort(u) {
		return false
	}
	if !strings.HasSuffix(u.Path, "/app/") || path.Clean(u.Path)+"/" != u.Path || u.EscapedPath() != u.Path {
		return false
	}
	return u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == ""
}

func canonicalPort(u *url.URL) bool {
	port := u.Port()
	if port == "" {
		return !strings.HasSuffix(u.Host, ":")
	}
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number <= 65535 && strconv.Itoa(number) == port
}
