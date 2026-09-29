package config

import (
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

const sandboxMode = "sandbox"
const zitadelMode = "zitadel"

type Zitadel struct {
	Provisioning      Provisioning `yaml:"provisioning"        json:"provisioning"`
	Issuer            string       `yaml:"issuer"              json:"issuer"`
	Audience          string       `yaml:"audience"            json:"audience"`
	BotID             string       `yaml:"bot_id"              json:"bot_id"`
	BotClientID       string       `yaml:"bot_client_id"       json:"bot_client_id"`
	BotClientSecret   Secret       `yaml:"bot_client_secret"   json:"bot_client_secret"`
	APIClientID       string       `yaml:"api_client_id"       json:"api_client_id"`
	APIClientSecret   Secret       `yaml:"api_client_secret"   json:"api_client_secret"`
	ActorID           string       `yaml:"actor_id"            json:"actor_id"`
	ActorClientID     string       `yaml:"actor_client_id"     json:"actor_client_id"`
	ActorClientSecret Secret       `yaml:"actor_client_secret" json:"actor_client_secret"`
}

func (z Zitadel) AdapterConfig(environment string) identity.ZitadelConfig {
	return identity.ZitadelConfig{
		Issuer: z.Issuer, Audience: z.Audience,
		BotClientID: z.BotClientID, BotClientSecret: z.BotClientSecret.Value(),
		APIClientID: z.APIClientID, APIClientSecret: z.APIClientSecret.Value(),
		ActorID: z.ActorID, ActorClientID: z.ActorClientID, ActorClientSecret: z.ActorClientSecret.Value(),
		Sandbox: environment == sandboxMode,
	}
}

func (a Auth) validate(environment string) error {
	if _, err := identity.BrowserOrigins(a.LegacyBrowserOrigins); err != nil {
		return errDiagnosticBrowserOrigins
	}
	switch a.Mode {
	case "", sandboxMode:
		if a.Zitadel != (Zitadel{}) {
			return errDiagnosticAuthConfiguration
		}
		return nil
	case zitadelMode:
		botID, err := strconv.ParseInt(a.Zitadel.BotID, 10, 64)
		if err != nil || botID <= 0 {
			return errDiagnosticBotID
		}
		if strings.TrimSpace(a.Zitadel.BotClientID) == strings.TrimSpace(a.Zitadel.APIClientID) {
			return errDiagnosticSeparateApps
		}
		_, err = identity.NewZitadel(a.Zitadel.AdapterConfig(environment))
		if err != nil {
			if environment == productionMode {
				return errDiagnosticProductionZitadel
			}
			return errDiagnosticZitadel
		}
		return nil
	default:
		return errDiagnosticAuthConfiguration
	}
}
