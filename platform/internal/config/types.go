// Package config loads validated process settings without ambient environment or I/O.
package config

import (
	"log/slog"
	"time"
)

// Secret requires an explicit Value call at the consuming boundary. Common log
// and JSON formatting redact it; configuration is not an export format for secrets.
const redacted = "[redacted]"
const defaultHistoryRecent = 12

const DefaultAssistantDailyLimit = 50
const MaxAssistantDailyLimit = 10000

type Secret string

func (s Secret) Value() string              { return string(s) }
func (Secret) String() string               { return redacted }
func (Secret) GoString() string             { return redacted }
func (Secret) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }
func (Secret) MarshalYAML() (any, error)    { return redacted, nil }

type Config struct {
	Registration     Registration     `yaml:"registration"      json:"registration"`
	Delivery         Delivery         `yaml:"delivery"          json:"delivery"`
	Credits          Credits          `yaml:"credits"           json:"credits"`
	AssistantSources AssistantSources `yaml:"assistant_sources" json:"assistant_sources"`
	Lineup           Lineup           `yaml:"lineup"            json:"lineup"`
	Env              string           `yaml:"env"               json:"env"`
	ParentStdin      bool             `yaml:"parent_stdin"      json:"parent_stdin"`
	SyntheticOnly    bool             `yaml:"synthetic_only"    json:"synthetic_only"`
	Server           Server           `yaml:"server"            json:"server"`
	Database         Database         `yaml:"database"          json:"database"`
	Auth             Auth             `yaml:"auth"              json:"auth"`
	Core             Core             `yaml:"core"              json:"core"`
	Telegram         Telegram         `yaml:"telegram"          json:"telegram"`
	Sandbox          Sandbox          `yaml:"sandbox"           json:"sandbox"`
	Model            Model            `yaml:"model"             json:"model"`
	Media            Worker           `yaml:"media"             json:"media"`
	Sticker          Sticker          `yaml:"sticker"           json:"sticker"`
	Script           Script           `yaml:"script"            json:"script"`
	History          History          `yaml:"history"           json:"history"`
	Orders           Orders           `yaml:"orders"            json:"orders"`
	Shutdown         Shutdown         `yaml:"shutdown"          json:"shutdown"`
	OTel             OTel             `yaml:"otel"              json:"otel"`
	Log              Log              `yaml:"log"               json:"log"`
}

const defaultRegistrationRetention = 10 * time.Minute

type Registration struct {
	Retention time.Duration `yaml:"retention" json:"retention"`
}

type Credits struct {
	Enforce bool `yaml:"enforce" json:"enforce"`
}

type Log struct {
	Level string `yaml:"level" json:"level"`
}

type Script struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Socket  string `yaml:"socket"  json:"socket"`
}

type History struct {
	Recent int `yaml:"recent" json:"recent"`
}

type Server struct {
	Host              string        `yaml:"host"                json:"host"`
	Port              int           `yaml:"port"                json:"port"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" json:"read_header_timeout"`
	ReadTimeout       time.Duration `yaml:"read_timeout"        json:"read_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout"       json:"write_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout"        json:"idle_timeout"`
	HealthTimeout     time.Duration `yaml:"health_timeout"      json:"health_timeout"`
}
type Database struct {
	URL Secret `yaml:"url" json:"url"`
}
type Auth struct {
	SandboxTelegramOwners map[int64]string `yaml:"sandbox_telegram_owners" json:"sandbox_telegram_owners"`
	LegacyBrowserOrigins  string           `yaml:"legacy_browser_origins"  json:"legacy_browser_origins"`
	Mode                  string           `yaml:"mode"                    json:"mode"`
	Zitadel               Zitadel          `yaml:"zitadel"                 json:"zitadel"`
	SigningKey            Secret           `yaml:"signing_key"             json:"signing_key"`
}
type Core struct {
	URL string `yaml:"url" json:"url"`
}
type Telegram struct {
	BaseURL   string `yaml:"base_url"    json:"base_url"`
	Token     Secret `yaml:"token"       json:"token"`
	WebAppURL string `yaml:"web_app_url" json:"web_app_url"`
}
type Sandbox struct {
	MiniAppURL string `yaml:"mini_app_url" json:"mini_app_url"`
}
type Model struct {
	RemoteSecret        Secret `yaml:"remote_secret"         json:"remote_secret"`
	AssistantDailyLimit int    `yaml:"assistant_daily_limit" json:"assistant_daily_limit"`
	Provider            string `yaml:"provider"              json:"provider"`
	URL                 string `yaml:"url"                   json:"url"`
	OpenAIKey           Secret `yaml:"openai_key"            json:"openai_key"`
	CodexExecutable     string `yaml:"codex_executable"      json:"codex_executable"`
}
type Worker struct {
	URL    string `yaml:"url"    json:"url"`
	Secret Secret `yaml:"secret" json:"secret"`
}
type Sticker struct {
	Worker Worker `yaml:"worker" json:"worker"`
	Cache  Cache  `yaml:"cache"  json:"cache"`
}
type Cache struct {
	Capacity int           `yaml:"capacity"  json:"capacity"`
	HalfLife time.Duration `yaml:"half_life" json:"half_life"`
}
type Orders struct {
	ActiveEvent   string        `yaml:"active_event"   json:"active_event"`
	ReminderAfter time.Duration `yaml:"reminder_after" json:"reminder_after"`
}
type Shutdown struct {
	Drain          time.Duration `yaml:"drain"           json:"drain"`
	TelemetryFlush time.Duration `yaml:"telemetry_flush" json:"telemetry_flush"`
}

// OTel describes the planned OTLP/HTTP exporter; loading settings starts no exporter.
type OTel struct {
	Enabled     bool    `yaml:"enabled"      json:"enabled"`
	Endpoint    string  `yaml:"endpoint"     json:"endpoint"`
	SampleRatio float64 `yaml:"sample_ratio" json:"sample_ratio"`
}

func defaults(command string) Config {
	const defaultPort = 8080
	const defaultCapacity = 5000
	const defaultSampleRatio = 0.1
	const readHeader = 5 * time.Second
	const read = 15 * time.Second
	const write = 45 * time.Second
	const idle = 30 * time.Second
	const health = 3 * time.Second
	const drain = 5 * time.Second
	const reminder = 48 * time.Hour
	const halfLife = 7 * 24 * time.Hour
	provider := providerRemote
	if command == "model" || command == "app" {
		provider = providerOpenAI
	}
	return Config{
		Server: Server{
			Port:              defaultPort,
			ReadHeaderTimeout: readHeader,
			ReadTimeout:       read,
			WriteTimeout:      write,
			IdleTimeout:       idle,
			HealthTimeout:     health,
		},
		Telegram:     Telegram{Token: Secret(sandboxMode), WebAppURL: "http://127.0.0.1:8090/miniapp/"},
		Delivery:     defaultDelivery(),
		Registration: Registration{Retention: defaultRegistrationRetention},
		Sandbox:      Sandbox{MiniAppURL: "http://bot:8080"},
		Model:        Model{Provider: provider, AssistantDailyLimit: DefaultAssistantDailyLimit},
		Sticker: Sticker{
			Cache: Cache{Capacity: defaultCapacity, HalfLife: halfLife},
		},
		Orders:   Orders{ReminderAfter: reminder},
		Shutdown: Shutdown{Drain: drain, TelemetryFlush: drain},
		OTel:     OTel{SampleRatio: defaultSampleRatio},
		Log:      Log{Level: "info"},
		History:  History{Recent: defaultHistoryRecent},
	}
}
