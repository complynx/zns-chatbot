package db

import (
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/internal/miniapp"
)

const (
	miniAppDefaultAge     = 300
	miniAppMinAge         = 30
	miniAppMaxAge         = int(miniapp.MaxLoginAge / time.Second)
	miniAppDefaultPending = 120
	miniAppMinPending     = 30
	miniAppMaxPending     = 300
	miniAppDefaultSession = 900
	miniAppMinSession     = 60
	miniAppMaxSession     = 3600
)

type MiniAppPolicy struct{ MaxAge, PendingTTL, SessionTTL time.Duration }

// MiniAppPolicy supplies backward-compatible defaults for old settings documents.
func (s Settings) MiniAppPolicy() (MiniAppPolicy, error) {
	age, pending, session := s.MiniAppMaxAgeSeconds, s.MiniAppPendingSeconds, s.MiniAppSessionSeconds
	if age == 0 {
		age = miniAppDefaultAge
	}
	if pending == 0 {
		pending = miniAppDefaultPending
	}
	if session == 0 {
		session = miniAppDefaultSession
	}
	if age < miniAppMinAge || age > miniAppMaxAge || pending < miniAppMinPending || pending > miniAppMaxPending ||
		session < miniAppMinSession || session > miniAppMaxSession {
		return MiniAppPolicy{}, errors.New("invalid mini app session policy")
	}
	return MiniAppPolicy{MaxAge: time.Duration(age) * time.Second, PendingTTL: time.Duration(pending) * time.Second,
		SessionTTL: time.Duration(session) * time.Second}, nil
}
