package config

import "log/slog"

// diagnostic contains only a closed code. It cannot retain configuration values
// or provider errors. LogValue lets startup report these static validation facts
// while the normal logger continues to redact arbitrary errors.
type diagnostic uint8

const (
	errDiagnosticUnknown diagnostic = iota
	errDiagnosticModelServer
	errDiagnosticCommand
	errDiagnosticSynthetic
	errDiagnosticDatabaseURL
	errDiagnosticDatabasePassword
	errDiagnosticDatabaseOverride
	errDiagnosticAuthMode
	errDiagnosticSigningSecret
	errDiagnosticBotSecret
	errDiagnosticAPISecret
	errDiagnosticActorSecret
	errDiagnosticModelProvider
	errDiagnosticMediaSecret
	errDiagnosticStickerSecret
	errDiagnosticPublicURL
	errDiagnosticBrowserOrigins
	errDiagnosticTelegramOrigin
	errDiagnosticTelegramToken
	errDiagnosticAuthConfiguration
	errDiagnosticBotID
	errDiagnosticSeparateApps
	errDiagnosticZitadel
	errDiagnosticProductionZitadel
	errDiagnosticSigningLength
	errDiagnosticBotNamespace
)

type diagnosticInfo struct {
	code   string
	field  string
	reason string
}

func (d diagnostic) Error() string { return d.info().reason }

func (d diagnostic) LogValue() slog.Value {
	info := d.info()
	return slog.GroupValue(slog.String("code", info.code), slog.String("field", info.field),
		slog.String("reason", info.reason))
}

func (d diagnostic) info() diagnosticInfo {
	const databaseField = "database_url"
	const zitadelField = "auth_zitadel"
	info := [...]diagnosticInfo{
		errDiagnosticUnknown: {"invalid_configuration", "configuration", "invalid configuration"},
		errDiagnosticModelServer: {"production_model_boundary", "command",
			"production model server requires an authenticated service boundary; use app with openai"},
		errDiagnosticCommand: {"production_command", "command", "production forbids sandbox fixture and fake commands"},
		errDiagnosticSynthetic: {"production_synthetic", "synthetic_only_parent_stdin",
			"production forbids synthetic_only and parent_stdin"},
		errDiagnosticDatabaseURL: {"production_database_url", databaseField,
			"production database requires an explicit PostgreSQL URL with credentials"},
		errDiagnosticDatabasePassword: {"production_database_password", databaseField,
			"production database requires an explicit nonfixture password"},
		errDiagnosticDatabaseOverride: {"production_database_override", databaseField,
			"production database credentials must not be overridden by URL parameters"},
		errDiagnosticModelProvider: {"production_model_provider", "model_provider_openai_key",
			"production runtime requires openai with an explicit nonfixture key; remote is not authenticated"},
		errDiagnosticMediaSecret: {"production_media_secret", "media_secret",
			"production media requires an explicit nonfixture secret"},
		errDiagnosticStickerSecret: {"production_sticker_secret", "sticker_worker_secret",
			"production sticker worker requires an explicit nonfixture secret"},
		errDiagnosticPublicURL: {"production_public_url", "telegram_web_app_url",
			"production telegram.web_app_url must be a public HTTPS URL"},
		errDiagnosticTelegramOrigin: {"production_telegram_origin", "telegram_base_url",
			"production telegram.base_url must use the official Telegram HTTPS origin"},
		errDiagnosticTelegramToken: {"production_telegram_token", "telegram_token",
			"production telegram.token must contain the configured bot ID and an explicit nonfixture secret"},
		errDiagnosticAuthMode: {"production_auth_mode", "auth_mode", "production requires auth.mode=zitadel"},
		errDiagnosticSigningSecret: {"production_signing_secret", "auth_signing_key",
			"production auth.signing_key requires an explicit nonfixture secret"},
		errDiagnosticBotSecret: {"production_bot_secret", "auth_zitadel_bot_client_secret",
			"production Zitadel bot application requires an explicit nonfixture secret"},
		errDiagnosticAPISecret: {"production_api_secret", "auth_zitadel_api_client_secret",
			"production Zitadel API application requires an explicit nonfixture secret"},
		errDiagnosticActorSecret: {"production_actor_secret", "auth_zitadel_actor_client_secret",
			"production Zitadel actor requires an explicit nonfixture secret"},
		errDiagnosticBrowserOrigins: {"auth_browser_origins", "auth_legacy_browser_origins",
			"legacy browser origins must be valid HTTPS origins; production requires public origins"},
		errDiagnosticAuthConfiguration: {"auth_configuration", "auth_mode",
			"configuration auth.mode must be sandbox or zitadel; auth.zitadel requires auth.mode=zitadel"},
		errDiagnosticBotID: {"auth_bot_id", "auth_zitadel_bot_id",
			"configuration Zitadel bot ID must be a positive Telegram bot ID"},
		errDiagnosticSeparateApps: {"auth_separate_apps", zitadelField,
			"configuration Zitadel bot and API applications must be separate"},
		errDiagnosticZitadel: {"auth_zitadel_configuration", zitadelField,
			"Zitadel requires a valid issuer origin, audience, application credentials and actor credentials"},
		errDiagnosticProductionZitadel: {
			"production_zitadel_configuration",
			zitadelField,
			"production Zitadel requires an HTTPS issuer origin, audience, application credentials and actor credentials",
		},
		errDiagnosticSigningLength: {"auth_signing_length", "auth_signing_key",
			"configuration auth.signing_key requires at least 32 bytes"},
		errDiagnosticBotNamespace: {"auth_bot_namespace", "auth_zitadel_bot_id_telegram_token",
			"configured bot ID must be positive and match the Telegram token identity"},
	}
	if int(d) >= len(info) {
		return info[errDiagnosticUnknown]
	}
	return info[d]
}
