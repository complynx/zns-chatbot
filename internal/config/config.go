// Package config loads startup parameters. Runtime business settings belong in PostgreSQL.
package config

import (
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/complynx/zns-chatbot/internal/miniapp"
)

const registrationDetailsKeyLength = 32

const defaultHTTPTimeout = 10 * time.Second
const defaultTraceRatio = 0.1
const maxTokenLength = 256
const defaultTraceQueueSize = 256
const defaultTraceTimeout = 250 * time.Millisecond

type Telegram struct {
	Token         string `yaml:"token"`
	BotID         int64  `yaml:"-"`
	BaseURL       string `yaml:"base_url"`
	WebhookSecret string `yaml:"webhook_secret"`
}
type Loom struct {
	BaseURL       string `yaml:"base_url"`
	SkillsEnabled bool   `yaml:"skills_enabled"`
}
type OTel struct {
	Endpoint     string        `yaml:"endpoint"`
	ServiceName  string        `yaml:"service_name"`
	SampleRatio  float64       `yaml:"sample_ratio"`
	MaxQueueSize int           `yaml:"max_queue_size"`
	Timeout      time.Duration `yaml:"timeout"`
}
type Identity struct {
	Issuer                    string `yaml:"issuer"`
	OrganizationID            string `yaml:"organization_id"`
	Audience                  string `yaml:"audience"`
	APIAudience               string `yaml:"api_audience"`
	ActorID                   string `yaml:"actor_id"`
	ActorClientID             string `yaml:"actor_client_id"`
	ActorClientSecret         string `yaml:"actor_client_secret"`
	ClientID                  string `yaml:"client_id"`
	ClientSecret              string `yaml:"client_secret"`
	IntrospectionClientID     string `yaml:"introspection_client_id"`
	IntrospectionClientSecret string `yaml:"introspection_client_secret"`
}

// TelegramPrelink is startup-only provider trust and a dedicated service secret.
type TelegramPrelink struct {
	Issuer        string `yaml:"issuer"`
	JWKSURL       string `yaml:"jwks_url"`
	Audience      string `yaml:"audience"`
	ProviderID    string `yaml:"provider_id"`
	ServiceBearer string `yaml:"service_bearer"`
}

type Config struct {
	MiniAppLaunchURL       string          `yaml:"mini_app_launch_url"`
	MiniAppAllowLocalHTTP  bool            `yaml:"mini_app_allow_local_http"`
	RegistrationDetailsKey string          `yaml:"registration_details_key"`
	TelegramPrelink        TelegramPrelink `yaml:"telegram_prelink"`
	MiniAppOrigin          string          `yaml:"mini_app_origin"`
	AvatarExecutable       string          `yaml:"avatar_executable"`
	DatabaseURL            string          `yaml:"database_url"`
	HTTPAddress            string          `yaml:"http_address"`
	InternalAddress        string          `yaml:"internal_address"`
	HTTPTimeout            time.Duration   `yaml:"http_timeout"`
	ShutdownTimeout        time.Duration   `yaml:"shutdown_timeout"`
	Telegram               Telegram        `yaml:"telegram"`
	Loom                   Loom            `yaml:"loom"`
	Identity               Identity        `yaml:"identity"`
	OTel                   OTel            `yaml:"otel"`
}

func Load(path string) (Config, error) {
	c := Config{
		AvatarExecutable: "/usr/local/bin/zinusya-avatar",
		HTTPAddress:      "127.0.0.1:8080",
		InternalAddress:  "127.0.0.1:9090",
		HTTPTimeout:      defaultHTTPTimeout,
		ShutdownTimeout:  time.Second,
		Telegram:         Telegram{BaseURL: "https://api.telegram.org"},
		OTel: OTel{
			ServiceName:  "zinusya",
			SampleRatio:  defaultTraceRatio,
			MaxQueueSize: defaultTraceQueueSize,
			Timeout:      defaultTraceTimeout,
		},
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, errors.New("cannot open startup configuration")
	}
	defer f.Close()
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return Config{}, errors.New("invalid startup configuration YAML")
	}
	var extra any
	if err = d.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("startup configuration must contain one YAML document")
	}
	fields := map[string]*string{
		"TELEGRAM_PRELINK_ISSUER":              &c.TelegramPrelink.Issuer,
		"TELEGRAM_PRELINK_JWKS_URL":            &c.TelegramPrelink.JWKSURL,
		"TELEGRAM_PRELINK_AUDIENCE":            &c.TelegramPrelink.Audience,
		"TELEGRAM_PRELINK_PROVIDER_ID":         &c.TelegramPrelink.ProviderID,
		"TELEGRAM_PRELINK_SERVICE_BEARER":      &c.TelegramPrelink.ServiceBearer,
		"REGISTRATION_DETAILS_KEY":             &c.RegistrationDetailsKey,
		"MINI_APP_ORIGIN":                      &c.MiniAppOrigin,
		"MINI_APP_LAUNCH_URL":                  &c.MiniAppLaunchURL,
		"AVATAR_EXECUTABLE":                    &c.AvatarExecutable,
		"DATABASE_URL":                         &c.DatabaseURL,
		"HTTP_ADDRESS":                         &c.HTTPAddress,
		"INTERNAL_ADDRESS":                     &c.InternalAddress,
		"TELEGRAM_TOKEN":                       &c.Telegram.Token,
		"TELEGRAM_BASE_URL":                    &c.Telegram.BaseURL,
		"TELEGRAM_WEBHOOK_SECRET":              &c.Telegram.WebhookSecret,
		"LOOM_BASE_URL":                        &c.Loom.BaseURL,
		"IDENTITY_ISSUER":                      &c.Identity.Issuer,
		"IDENTITY_ORGANIZATION_ID":             &c.Identity.OrganizationID,
		"IDENTITY_AUDIENCE":                    &c.Identity.Audience,
		"IDENTITY_API_AUDIENCE":                &c.Identity.APIAudience,
		"IDENTITY_ACTOR_ID":                    &c.Identity.ActorID,
		"IDENTITY_ACTOR_CLIENT_ID":             &c.Identity.ActorClientID,
		"IDENTITY_ACTOR_CLIENT_SECRET":         &c.Identity.ActorClientSecret,
		"IDENTITY_CLIENT_ID":                   &c.Identity.ClientID,
		"IDENTITY_CLIENT_SECRET":               &c.Identity.ClientSecret,
		"IDENTITY_INTROSPECTION_CLIENT_ID":     &c.Identity.IntrospectionClientID,
		"IDENTITY_INTROSPECTION_CLIENT_SECRET": &c.Identity.IntrospectionClientSecret,
		"OTEL_ENDPOINT":                        &c.OTel.Endpoint,
		"OTEL_SERVICE_NAME":                    &c.OTel.ServiceName,
	}
	for key, field := range fields {
		if value, ok := os.LookupEnv("SHUTTLE_" + key); ok {
			*field = value
		}
	}
	if err = applyTypedEnvironment(&c); err != nil {
		return Config{}, err
	}
	botID, _, found := strings.Cut(c.Telegram.Token, ":")
	if found {
		c.Telegram.BotID, _ = strconv.ParseInt(botID, 10, 64)
	}
	if err = c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) Validate() error {
	for _, check := range []func() error{
		c.validateTimeouts,
		c.validateListeners,
		c.validateServices,
		c.validateTelegram,
		c.validateIdentity,
		c.validateTelegramPrelink,
		c.validateRegistrationDetails,
		c.OTel.validate,
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}
func (c Config) validateTimeouts() error {
	if c.HTTPTimeout <= 0 || c.HTTPTimeout > time.Minute {
		return errors.New("HTTP timeout must be within (0, 1m]")
	}
	if c.ShutdownTimeout <= 0 || c.ShutdownTimeout > 5*time.Second {
		return errors.New("shutdown timeout must be within (0, 5s]")
	}
	return nil
}
func (c Config) validateListeners() error {
	for _, address := range []string{c.HTTPAddress, c.InternalAddress} {
		_, port, splitErr := net.SplitHostPort(address)
		p, portErr := strconv.Atoi(port)
		if splitErr != nil || portErr != nil || p <= 0 || p > 65535 {
			return errors.New("invalid listener address")
		}
	}
	return nil
}
func (c Config) validateServices() error {
	if c.MiniAppOrigin != "" && !miniapp.ValidSessionOrigin(c.MiniAppOrigin, c.MiniAppAllowLocalHTTP) {
		return errors.New("mini app origin requires HTTPS; localhost HTTP requires explicit development opt-in")
	}
	if c.MiniAppLaunchURL != "" && (c.MiniAppOrigin == "" || !miniapp.ValidLaunchURL(c.MiniAppLaunchURL)) {
		return errors.New("mini app launch URL requires an enabled origin and a canonical HTTPS URL ending in /app/")
	}
	if c.MiniAppOrigin != "" && !miniapp.ValidLaunchURL(c.MiniAppURL()) {
		return errors.New("mini app requires a canonical HTTPS launch URL; local HTTP needs an explicit launch URL")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return errors.New("invalid PostgreSQL URL")
	}
	if !validURL(c.Telegram.BaseURL, false) || !validURL(c.Identity.Issuer, false) || !validURL(c.Loom.BaseURL, true) {
		return errors.New("invalid startup service URL")
	}
	return nil
}
func (c OTel) validate() error {
	if c.Endpoint != "" && !validURL(c.Endpoint, true) {
		return errors.New("invalid OTel endpoint")
	}
	if c.ServiceName == "" || len(c.ServiceName) > 128 {
		return errors.New("invalid OTel service name")
	}
	if math.IsNaN(c.SampleRatio) || c.SampleRatio < 0 || c.SampleRatio > 1 {
		return errors.New("invalid OTel sampling ratio")
	}
	if c.MaxQueueSize < 1 || c.MaxQueueSize > 65536 {
		return errors.New("invalid OTel queue size")
	}
	if c.Timeout <= 0 || c.Timeout > time.Second {
		return errors.New("invalid OTel export timeout")
	}
	return nil
}
func (c Config) validateTelegram() error {
	botID, secret, found := strings.Cut(c.Telegram.Token, ":")
	if !found || c.Telegram.BotID <= 0 || c.Telegram.BotID >= 1<<52 ||
		botID != strconv.FormatInt(c.Telegram.BotID, 10) ||
		len(c.Telegram.Token) > maxTokenLength {
		return errors.New("invalid Telegram bot token")
	}
	if !validSecret(secret) {
		return errors.New("invalid Telegram bot token")
	}
	if len(c.Telegram.WebhookSecret) < 16 || len(c.Telegram.WebhookSecret) > 256 ||
		!validSecret(c.Telegram.WebhookSecret) {
		return errors.New("invalid Telegram webhook secret")
	}
	return nil
}
func validSecret(secret string) bool {
	if secret == "" {
		return false
	}
	for _, ch := range secret {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", ch) {
			return false
		}
	}
	return true
}
func (c Config) validateIdentity() error {
	for _, value := range []string{
		c.Identity.OrganizationID,
		c.Identity.Audience,
		c.Identity.APIAudience,
		c.Identity.ActorID,
		c.Identity.ActorClientID,
		c.Identity.ActorClientSecret,
		c.Identity.ClientID,
		c.Identity.ClientSecret,
		c.Identity.IntrospectionClientID,
		c.Identity.IntrospectionClientSecret,
	} {
		if strings.TrimSpace(value) == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n") {
			return errors.New("required identity startup parameter missing")
		}
	}
	return nil
}

func validURL(endpoint string, allowHTTP bool) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(endpoint) > 2048 {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return allowHTTP || u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
}

func (c Config) validateTelegramPrelink() error {
	p := c.TelegramPrelink
	if p == (TelegramPrelink{}) {
		return nil
	}
	for _, endpoint := range []string{p.Issuer, p.JWKSURL} {
		u, err := url.Parse(endpoint)
		if err != nil || !validURL(endpoint, false) || u.Scheme != "https" || u.Hostname() == "" || u.ForceQuery ||
			u.Opaque != "" {
			return errors.New("prelink HTTPS issuer and JWKS URL required")
		}
	}
	for _, value := range []string{p.Audience, p.ProviderID} {
		if value == "" || len(value) > 200 || !utf8.ValidString(value) ||
			strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return errors.New("prelink audience and provider required")
		}
	}
	if p.Audience == c.Identity.Audience || p.Audience == c.Identity.APIAudience {
		return errors.New("prelink audience must be dedicated")
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(p.ServiceBearer)
	if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != p.ServiceBearer {
		return errors.New("prelink requires a dedicated base64url 32-byte service key")
	}
	if slices.Contains([]string{c.Telegram.Token, c.Telegram.WebhookSecret, c.Identity.ActorClientSecret,
		c.Identity.ClientSecret, c.Identity.IntrospectionClientSecret}, p.ServiceBearer) {
		return errors.New("prelink service key must not reuse another credential")
	}
	return nil
}

// RegistrationDetailsSecret is optional and has no generated or derived default.
// Keep it with database backups; replacing it breaks decryption and write replay.
func (c Config) RegistrationDetailsSecret() ([]byte, error) {
	if c.RegistrationDetailsKey == "" {
		return nil, nil
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(c.RegistrationDetailsKey)
	if err != nil || len(key) != registrationDetailsKeyLength ||
		base64.RawURLEncoding.EncodeToString(key) != c.RegistrationDetailsKey {
		return nil, errors.New("registration details require a persistent base64url 32-byte key")
	}
	return key, nil
}

func (c Config) validateRegistrationDetails() error {
	_, err := c.RegistrationDetailsSecret()
	return err
}

// MiniAppURL is the bot's launch link, independently of the session trust origin.
func (c Config) MiniAppURL() string {
	if c.MiniAppOrigin == "" {
		return ""
	}
	if c.MiniAppLaunchURL != "" {
		return c.MiniAppLaunchURL
	}
	return c.MiniAppOrigin + "/app/"
}

// applyTypedEnvironment preserves parsing order and keeps startup errors free of values.
func applyTypedEnvironment(c *Config) error {
	if value, ok := os.LookupEnv("SHUTTLE_LOOM_SKILLS_ENABLED"); ok {
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return errors.New("invalid SHUTTLE_LOOM_SKILLS_ENABLED")
		}
		c.Loom.SkillsEnabled = parsed
	}
	for key, field := range map[string]*time.Duration{
		"HTTP_TIMEOUT":     &c.HTTPTimeout,
		"SHUTDOWN_TIMEOUT": &c.ShutdownTimeout,
		"OTEL_TIMEOUT":     &c.OTel.Timeout,
	} {
		if value, ok := os.LookupEnv("SHUTTLE_" + key); ok {
			parsed, parseErr := time.ParseDuration(value)
			if parseErr != nil {
				return errors.New("invalid SHUTTLE_" + key)
			}
			*field = parsed
		}
	}
	if value, ok := os.LookupEnv("SHUTTLE_MINI_APP_ALLOW_LOCAL_HTTP"); ok {
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return errors.New("invalid SHUTTLE_MINI_APP_ALLOW_LOCAL_HTTP")
		}
		c.MiniAppAllowLocalHTTP = parsed
	}
	if value, ok := os.LookupEnv("SHUTTLE_OTEL_SAMPLE_RATIO"); ok {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil {
			return errors.New("invalid SHUTTLE_OTEL_SAMPLE_RATIO")
		}
		c.OTel.SampleRatio = parsed
	}
	if value, ok := os.LookupEnv("SHUTTLE_OTEL_MAX_QUEUE_SIZE"); ok {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return errors.New("invalid SHUTTLE_OTEL_MAX_QUEUE_SIZE")
		}
		c.OTel.MaxQueueSize = parsed
	}
	return nil
}
