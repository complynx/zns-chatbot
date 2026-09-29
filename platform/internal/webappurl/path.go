// Package webappurl maps internal routes to the configured public Mini App mount.
package webappurl

import (
	"net/url"
	"strings"
)

// Route replaces the Mini App route while preserving its configured mount.
// Other configured entry paths retain the historical root-mounted behavior.
func Route(publicURL, route string) (*url.URL, error) {
	address, err := url.Parse(publicURL)
	if err != nil {
		return nil, err
	}
	prefix, mounted := strings.CutSuffix(strings.TrimRight(address.Path, "/"), "/miniapp")
	if !mounted {
		prefix = ""
	}
	rawPrefix, _ := strings.CutSuffix(strings.TrimRight(address.EscapedPath(), "/"), "/miniapp")
	address.Path = prefix + route
	address.RawPath, address.RawQuery, address.Fragment, address.RawFragment = "", "", "", ""
	if mounted && rawPrefix != prefix {
		address.RawPath = rawPrefix + route
	}
	return address, nil
}

// Path returns an escaped public path. Invalid configuration falls back to root.
func Path(publicURL, route string) string {
	address, err := Route(publicURL, route)
	if err != nil {
		return route
	}
	return address.EscapedPath()
}
