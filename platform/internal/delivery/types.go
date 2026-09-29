// Package delivery owns durable transport pacing, not message payloads or eligibility.
package delivery

import (
	"errors"
	"time"
)

var ErrSettings = errors.New("delivery identity or pacing settings missing")

type Settings struct {
	BotID        int64
	BotInterval  time.Duration
	ChatInterval time.Duration
	Fallback     time.Duration
}

// Validate rejects missing composition instead of silently inventing a bot scope.
func (s Settings) Validate() error {
	if s.BotID <= 0 || s.BotInterval <= 0 || s.ChatInterval <= 0 || s.Fallback <= 0 {
		return ErrSettings
	}
	return nil
}

type Destination struct {
	Chat   string
	Thread int64
}

type Kind string

const (
	Succeeded Kind = "sent"
	Deferred  Kind = "pending"
	Rejected  Kind = "failed"
	Cancelled Kind = "cancelled"
	Uncertain Kind = "unknown"
	Parked    Kind = "parked"
	Paused    Kind = "paused"
)

// Outcome describes one wire attempt. Generation fencing remains in its outbox.
type Outcome struct {
	Kind       Kind   `json:"kind"`
	MessageID  int64  `json:"message_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
	RetryAfter int64  `json:"retry_after,omitempty"`
	Missing    bool   `json:"retry_after_missing,omitempty"`
}

func (o Outcome) Valid() bool {
	switch o.Kind {
	case Sending:
		return false
	case Succeeded:
		return o.MessageID > 0 && o.Reason == "" && o.RetryAfter == 0 && !o.Missing
	case Deferred:
		return o.validFailure() && o.RetryAfter >= 0 && (!o.Missing || o.RetryAfter == 0)
	case Rejected, Cancelled, Uncertain, Parked, Paused:
		return o.validFailure() && o.RetryAfter == 0 && !o.Missing
	default:
		return false
	}
}

func (o Outcome) validFailure() bool {
	return o.MessageID == 0 && o.Reason != "" && len(o.Reason) <= 100
}

// Attempt is scoped by the domain-specific delivery endpoint, not a generic queue.
type Attempt struct {
	ID         int64 `json:"id"`
	Generation int64 `json:"attempt"`
}

type Admission struct {
	Ready     bool      `json:"ready"`
	NotBefore time.Time `json:"not_before,omitzero"`
	Reason    string    `json:"reason,omitempty"`
}
