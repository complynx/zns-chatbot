package config

import (
	"errors"
	"strings"
)

// Provisioning credentials are consumed only by the core API. Split bot config
// needs Enabled and the existing bot ID, but no management credentials.
type Provisioning struct {
	Authorizer     Authorizer `yaml:"authorizer"      json:"authorizer"`
	Enabled        bool       `yaml:"enabled"         json:"enabled"`
	OrganizationID string     `yaml:"organization_id" json:"organization_id"`
	EmailDomain    string     `yaml:"email_domain"    json:"email_domain"`
	ClientID       string     `yaml:"client_id"       json:"client_id"`
	ClientSecret   Secret     `yaml:"client_secret"   json:"client_secret"`
}

func (c Config) validateProvisioning(command string) error {
	p := c.Auth.Zitadel.Provisioning
	if !p.Enabled {
		if p != (Provisioning{}) {
			return errors.New("provisioning fields require provisioning.enabled")
		}
		return nil
	}
	if c.Auth.Mode != zitadelMode {
		return errors.New("provisioning requires auth.mode=zitadel")
	}
	if err := p.Authorizer.validate(c.Env); err != nil {
		return err
	}
	if p.EmailDomain != "" &&
		(!strings.HasSuffix(p.EmailDomain, ".invalid") || strings.ContainsAny(p.EmailDomain, " /:@\\\r\n")) {
		return errors.New("provisioning email_domain must be a reserved .invalid domain")
	}
	if (p.ClientID == "") != (p.ClientSecret == "") {
		return errors.New("provisioning client credentials must be supplied together")
	}
	if p.ClientID != "" &&
		(p.ClientID == c.Auth.Zitadel.ActorClientID || p.ClientID == c.Auth.Zitadel.BotClientID || p.ClientID == c.Auth.Zitadel.APIClientID) {
		return errors.New("provisioning requires separate client credentials")
	}
	if command == modeAPI || command == modeApp {
		if strings.TrimSpace(p.OrganizationID) == "" || p.EmailDomain == "" || p.ClientID == "" ||
			p.ClientSecret == "" {
			return errors.New("core provisioning requires organization_id, email_domain and client credentials")
		}
		if c.Env == productionMode && !productionSecret(p.ClientSecret) {
			return errors.New("production provisioning requires an explicit nonfixture client secret")
		}
	}
	return nil
}
