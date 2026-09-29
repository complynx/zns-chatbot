package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

const productionMode = "production"

// Production validation admits existing real adapters only. Provider reachability,
// Telegram getMe and provisioned user mappings need separate deployment checks.
func (c Config) validateProduction(command string) error {
	switch command {
	case modeApp, modeBot, modeAPI, modeMigrate, modeHealth:
	case modeModel:
		return errDiagnosticModelServer
	default:
		return errDiagnosticCommand
	}
	if c.SyntheticOnly || c.ParentStdin {
		return errDiagnosticSynthetic
	}
	if command == modeHealth {
		return nil
	}
	if err := c.validateProductionDatabase(); err != nil {
		return err
	}
	if command == modeMigrate {
		return nil
	}
	if err := c.validateProductionAuth(); err != nil {
		return err
	}
	if c.Model.Provider != providerOpenAI || !productionSecret(c.Model.OpenAIKey) {
		return errDiagnosticModelProvider
	}
	for _, item := range []struct {
		failure diagnostic
		worker  Worker
	}{{errDiagnosticMediaSecret, c.Media}, {errDiagnosticStickerSecret, c.Sticker.Worker}} {
		if item.worker.URL != "" && !productionSecret(item.worker.Secret) {
			return item.failure
		}
	}
	if command == modeApp || command == modeBot {
		return c.validateProductionBot()
	}
	return nil
}

func (c Config) validateProductionDatabase() error {
	u, err := url.Parse(c.Database.URL.Value())
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		u.Hostname() == "" || u.User == nil || u.User.Username() == "" || u.Path == "" || u.Fragment != "" {
		return errDiagnosticDatabaseURL
	}
	password, present := u.User.Password()
	if !present || !productionSecret(Secret(password)) {
		return errDiagnosticDatabasePassword
	}
	for key := range u.Query() {
		if strings.EqualFold(key, "password") || strings.EqualFold(key, "user") || strings.EqualFold(key, "passfile") {
			return errDiagnosticDatabaseOverride
		}
	}
	return nil
}

func (c Config) validateProductionAuth() error {
	if c.Auth.Mode != zitadelMode {
		return errDiagnosticAuthMode
	}
	for _, item := range []struct {
		failure diagnostic
		secret  Secret
	}{
		{errDiagnosticSigningSecret, c.Auth.SigningKey},
		{errDiagnosticBotSecret, c.Auth.Zitadel.BotClientSecret},
		{errDiagnosticAPISecret, c.Auth.Zitadel.APIClientSecret},
		{errDiagnosticActorSecret, c.Auth.Zitadel.ActorClientSecret},
	} {
		if !productionSecret(item.secret) {
			return item.failure
		}
	}
	if !publicHTTPSURL(c.Telegram.WebAppURL) {
		return errDiagnosticPublicURL
	}
	for origin := range strings.FieldsSeq(c.Auth.LegacyBrowserOrigins) {
		if !publicHTTPSURL(origin) {
			return errDiagnosticBrowserOrigins
		}
	}
	return nil
}

func (c Config) validateProductionBot() error {
	if c.Telegram.BaseURL != "https://api.telegram.org" {
		return errDiagnosticTelegramOrigin
	}
	prefix, secret, present := strings.Cut(c.Telegram.Token.Value(), ":")
	id, err := strconv.ParseInt(prefix, 10, 64)
	if !present || err != nil || id <= 0 || prefix != c.Auth.Zitadel.BotID || !productionSecret(Secret(secret)) {
		return errDiagnosticTelegramToken
	}
	return nil
}

// These checks reject bundled defaults and common placeholders, not prove that a
// credential belongs to a provider. Deployment must inject independently issued secrets.
func productionSecret(secret Secret) bool {
	value := secret.Value()
	const minimumLength = 16
	if len(value) < minimumLength || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{sandboxMode, "synthetic", modeFixture, "changeme", "change-me", "placeholder"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func publicHTTPSURL(value string) bool {
	if !httpURL(value, false) {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != httpsScheme {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate()
	}
	return strings.Contains(host, ".") && host != "localhost" && !strings.HasSuffix(host, ".localhost") &&
		!strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal")
}
