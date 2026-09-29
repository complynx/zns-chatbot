package config

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

const httpsScheme = "https"

// Authorizer trusts one relay and one configured Zitadel JWT identity provider.
type Authorizer struct {
	Issuer  string `yaml:"issuer"   json:"issuer"`
	JWKSURL string `yaml:"jwks_url" json:"jwks_url"`
	IDPID   string `yaml:"idp_id"   json:"idp_id"`
}

func (a Authorizer) validate(environment string) error {
	if a == (Authorizer{}) {
		return nil
	}
	if strings.TrimSpace(a.IDPID) == "" || !authorizerTrustURL(a.Issuer, environment) ||
		!authorizerTrustURL(a.JWKSURL, environment) {
		return errors.New("authorizer requires explicit issuer, jwks_url and idp_id")
	}
	return nil
}

func authorizerTrustURL(raw, environment string) bool {
	if publicHTTPSURL(raw) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || environment != sandboxMode || u.Host == "" || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return (u.Scheme == "http" || u.Scheme == httpsScheme) &&
		(u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}
