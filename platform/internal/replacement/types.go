// Package replacement owns the single-host application deployment barrier.
package replacement

import (
	"context"
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const componentApp = "app"
const defaultStopTimeout = 30 * time.Second
const defaultVerifyTimeout = 5 * time.Second
const defaultReadyTimeout = 90 * time.Second

const LabelInstallation = "net.complynx.zns.installation"
const LabelLaunch = "net.complynx.zns.launch"
const LabelComponent = "net.complynx.zns.component"
const StateStarting = "starting"
const StateRunning = "running"
const StateStopping = "stopping"
const StateStopped = "stopped"
const StateBlocked = "blocked"

var ErrUnknown = errors.New("replacement ownership is uncertain")
var ErrStopped = errors.New("managed runtime stopped")
var ErrDeadline = errors.New("replacement stop deadline exceeded")
var ErrConfiguration = errors.New("invalid replacement configuration")

// Components is the complete supported deployment group, in startup order.
func Components() []string {
	return []string{"evaluator", "media-decoder", "sticker-decoder", "media-broker", "sticker-broker", componentApp}
}

// Container is an exact Docker execution identity, not a service-name selector.
type Container struct {
	ID         string `json:"id"`
	Component  string `json:"component"`
	Launch     string `json:"launch"`
	Image      string `json:"image"`
	Created    string `json:"created"`
	Running    bool   `json:"running"`
	Restarting bool   `json:"restarting"`
	Paused     bool   `json:"paused"`
	PID        int    `json:"pid"`
	Health     string `json:"health"`
}

// Ledger is durable before any generation is started or stopped.
type Ledger struct {
	Version      int         `json:"version"`
	Installation string      `json:"installation"`
	Host         string      `json:"host"`
	Daemon       string      `json:"daemon"`
	Generation   uint64      `json:"generation"`
	Launch       string      `json:"launch"`
	State        string      `json:"state"`
	Containers   []Container `json:"containers"`
}

// Engine operates only on the configured group and exact container IDs.
type Engine interface {
	Identity(context.Context) (string, error)
	Inventory(context.Context) ([]Container, error)
	Create(context.Context, runtimeapp.Instance) ([]Container, error)
	Start(context.Context, []Container) error
	Stop(context.Context, []Container) error
	Kill(context.Context, []Container) error
	Remove(context.Context, []Container) error
}

// Sessions reports all sessions using the configured managed database roles.
type Sessions interface {
	Names(context.Context) ([]string, error)
}

// Journal persists deployment state before subsequent execution.
type Journal interface {
	Load() (Ledger, error)
	Save(Ledger) error
}

// Coordinator is the fixed single-host stop/start policy. Its caller owns the host lock.
type Coordinator struct {
	Engine        Engine
	Sessions      Sessions
	Journal       Journal
	Installation  string
	Host          string
	StopTimeout   time.Duration
	VerifyTimeout time.Duration
	ReadyTimeout  time.Duration
	PollInterval  time.Duration
	NewLaunch     func() (string, error)
}

// DefaultBudgets bounds shutdown, verification and initial readiness independently.
func (c *Coordinator) DefaultBudgets() {
	if c.StopTimeout == 0 {
		c.StopTimeout = defaultStopTimeout
	}
	if c.VerifyTimeout == 0 {
		c.VerifyTimeout = defaultVerifyTimeout
	}
	if c.ReadyTimeout == 0 {
		c.ReadyTimeout = defaultReadyTimeout
	}
	if c.PollInterval == 0 {
		c.PollInterval = time.Second
	}
}
