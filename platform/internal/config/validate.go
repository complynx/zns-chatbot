package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const modeApp = "app"
const modeBot = "bot"
const modeModel = "model"
const modeAPI = "api"
const modeMigrate = "migrate"
const modeHealth = "health"
const modeFixture = "fixture"
const providerOpenAI = "openai"
const providerRemote = "remote"

// Validate checks startup requirements. It does not contact services or inspect
// executables; those checks remain at their runtime connection/start boundaries.
func (c Config) Validate(command string) error {
	if c.Credits.Enforce && c.Model.Provider == providerRemote && consumesPaidModel(command) &&
		c.Model.RemoteSecret.Value() == "" {
		return errors.New("credit enforcement requires trusted direct model attribution or model.remote_secret")
	}
	if _, err := c.Log.SlogLevel(); err != nil {
		return err
	}
	switch command {
	case modeApp,
		modeBot,
		modeModel,
		modeAPI,
		"fake",
		modeMigrate,
		modeFixture,
		"product-fixture",
		"export-fixture",
		modeHealth:
	default:
		return errors.New("unknown configuration command")
	}
	if err := c.validateValues(); err != nil {
		return err
	}
	if err := c.validateProvisioning(command); err != nil {
		return err
	}
	if c.Env == productionMode {
		if err := c.validateProduction(command); err != nil {
			return err
		}
	}
	if command == modeHealth {
		return nil
	}
	if c.Env != sandboxMode && c.Env != productionMode {
		return errors.New("configuration env must be sandbox or production")
	}
	if command != modeModel && strings.TrimSpace(c.Database.URL.Value()) == "" {
		return errors.New("configuration database.url is required")
	}
	const minSigningBytes = 32
	if (command == modeAPI || command == modeBot || command == modeApp) && len(c.Auth.SigningKey) < minSigningBytes {
		return errDiagnosticSigningLength
	}
	if err := c.validateBot(command); err != nil {
		return err
	}
	if command == modeBot || command == modeModel || command == modeApp {
		return c.validateModel(command)
	}
	return nil
}

func consumesPaidModel(command string) bool {
	return command == modeApp || command == modeBot || command == modeAPI || command == modeModel
}

func (c Config) validateBot(command string) error {
	const maxEventIDLength = 128
	if command == modeBot || command == modeApp {
		if strings.TrimSpace(c.Orders.ActiveEvent) == "" || len(c.Orders.ActiveEvent) > maxEventIDLength ||
			strings.ContainsAny(c.Orders.ActiveEvent, "/\\?# \t\r\n") {
			return errors.New("configuration orders.active_event requires an event ID")
		}
		if c.Telegram.Token == "" {
			return errors.New("configuration telegram.token is required")
		}
		if command == modeBot && !httpURL(c.Core.URL, false) {
			return errors.New("configuration core.url must be an HTTP URL")
		}
		if c.Telegram.BaseURL != "" && !httpURL(c.Telegram.BaseURL, false) {
			return errors.New("configuration telegram.base_url must be an HTTP URL")
		}
	}
	return nil
}

func (c Config) validateValues() error {
	if err := c.AssistantSources.validate(); err != nil {
		return err
	}
	if _, err := c.LegacyOrderBotID(); err != nil {
		return errDiagnosticBotNamespace
	}
	if err := c.Lineup.validate(); err != nil {
		return err
	}
	if err := c.Auth.validate(c.Env); err != nil {
		return err
	}
	if c.Model.AssistantDailyLimit < 1 || c.Model.AssistantDailyLimit > MaxAssistantDailyLimit {
		return errors.New("configuration model.assistant_daily_limit must be between 1 and 10000")
	}
	const maxPort = 65535
	if c.Server.Port < 1 || c.Server.Port > maxPort {
		return errors.New("configuration server.port must be between 1 and 65535")
	}
	if net.ParseIP(c.Server.Host) == nil && strings.ContainsAny(c.Server.Host, "/\\?#@[]: \t\r\n") {
		return errors.New("configuration server.host must be a hostname, IP address or empty")
	}
	durations := map[string]time.Duration{
		"server.read_header_timeout": c.Server.ReadHeaderTimeout,
		"server.read_timeout":        c.Server.ReadTimeout,
		"server.write_timeout":       c.Server.WriteTimeout,
		"server.idle_timeout":        c.Server.IdleTimeout,
		"server.health_timeout":      c.Server.HealthTimeout,
		"shutdown.drain":             c.Shutdown.Drain,
		"shutdown.telemetry_flush":   c.Shutdown.TelemetryFlush,
		"orders.reminder_after":      c.Orders.ReminderAfter,
	}
	for name, value := range durations {
		if value <= 0 {
			return fmt.Errorf("configuration %s must be positive", name)
		}
	}
	if c.Sticker.Cache.Capacity < 1 || c.Sticker.Cache.HalfLife < time.Second {
		return errors.New("configuration sticker cache requires positive capacity and half_life of at least 1s")
	}
	if err := validWorker("media", c.Media); err != nil {
		return err
	}
	if err := validWorker("sticker.worker", c.Sticker.Worker); err != nil {
		return err
	}
	if err := c.validateWebAppURLs(); err != nil {
		return err
	}
	if err := c.validateTelemetry(); err != nil {
		return err
	}
	if err := c.Script.validate(); err != nil {
		return err
	}
	return c.History.validate()
}

func (c Config) validateTelemetry() error {
	if math.IsNaN(c.OTel.SampleRatio) || math.IsInf(c.OTel.SampleRatio, 0) || c.OTel.SampleRatio < 0 ||
		c.OTel.SampleRatio > 1 {
		return errors.New("configuration otel.sample_ratio must be between 0 and 1")
	}
	if c.OTel.Enabled && c.OTel.Endpoint == "" {
		return errors.New("configuration otel.endpoint is required when enabled")
	}
	if c.OTel.Endpoint != "" && !httpURL(c.OTel.Endpoint, false) {
		return errors.New("configuration otel.endpoint must be an OTLP HTTP URL without credentials")
	}
	return nil
}

func (h History) validate() error {
	const maxRecent = 30
	if h.Recent < 1 || h.Recent > maxRecent {
		return errors.New("configuration history.recent must be between 1 and 30")
	}
	return nil
}

func (s Script) validate() error {
	if s.Enabled && !filepath.IsAbs(s.Socket) {
		return errors.New("configuration script.socket must be absolute when enabled")
	}
	return nil
}

func (c Config) validateModel(command string) error {
	switch c.Model.Provider {
	case modeFixture:
		if command == modeModel || !c.SyntheticOnly || !httpURL(c.Model.URL, false) {
			return errors.New(
				"configuration fixture model requires synthetic_only and an explicit HTTP URL in app or bot mode",
			)
		}
	case "remote":
		if command == modeModel {
			return errors.New("configuration model mode does not support remote provider")
		}
		if !httpURL(c.Model.URL, false) {
			return errors.New("configuration model.url must be an HTTP URL")
		}
	case providerOpenAI:
		return c.validateOpenAIModel(command)
	case "scripted":
		if command == modeBot {
			return errors.New("configuration bot mode requires remote or codex model provider")
		}
	case "codex":
		if command == modeModel {
			return errors.New("configuration model mode does not support codex provider")
		}
		if !c.SyntheticOnly || c.Server.Host != "127.0.0.1" || !filepath.IsAbs(c.Model.CodexExecutable) {
			return errors.New(
				"configuration codex requires synthetic_only, loopback host and an absolute executable path",
			)
		}
	default:
		return errors.New("configuration model.provider is unsupported")
	}
	return nil
}

func validWorker(name string, worker Worker) error {
	if worker.URL == "" && worker.Secret == "" {
		return nil
	}
	if !httpURL(worker.URL, true) || strings.TrimSpace(worker.Secret.Value()) == "" ||
		strings.ContainsAny(worker.Secret.Value(), "\r\n") {
		return fmt.Errorf("configuration %s requires an HTTP origin and a nonempty single-line secret", name)
	}
	return nil
}

func httpURL(value string, origin bool) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == httpsScheme) && parsed.Hostname() != "" &&
		parsed.User == nil &&
		parsed.RawQuery == "" &&
		parsed.Fragment == "" &&
		(!origin || parsed.Path == "" || parsed.Path == "/")
}

func (c Config) validateOpenAIModel(command string) error {
	if command == modeBot && c.Env != productionMode {
		return errors.New("configuration bot mode requires remote or codex model provider")
	}
	if strings.TrimSpace(c.Model.OpenAIKey.Value()) == "" {
		return errors.New("configuration model.openai_key is required")
	}
	if command == modeModel && c.Model.RemoteSecret.Value() == "" {
		return errors.New("configuration model.remote_secret is required for paid model mode")
	}

	return nil
}
