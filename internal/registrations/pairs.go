package registrations

import (
	"time"

	"github.com/google/uuid"
)

// Invitation records directed consent. Pending invitations hold only the inviter.
type Invitation struct {
	ID                    uuid.UUID  `json:"id"`
	EventID               uuid.UUID  `json:"event_id"`
	InviterRegistrationID uuid.UUID  `json:"inviter_registration_id"`
	TargetUserID          uuid.UUID  `json:"target_user_id"`
	State                 string     `json:"state"`
	Version               int64      `json:"version"`
	CreatedAt             time.Time  `json:"created_at"`
	ExpiresAt             time.Time  `json:"expires_at"`
	DecidedAt             *time.Time `json:"decided_at,omitempty"`
	PairID                *uuid.UUID `json:"pair_id,omitempty"`
}

// Pair keeps consent history without combining either participant's pass or money.
// Participant reads must not append the other participant's financial records.
type Pair struct {
	ID                     uuid.UUID  `json:"id"`
	EventID                uuid.UUID  `json:"event_id"`
	LeaderRegistrationID   uuid.UUID  `json:"leader_registration_id"`
	FollowerRegistrationID uuid.UUID  `json:"follower_registration_id"`
	State                  string     `json:"state"`
	Version                int64      `json:"version"`
	QueuedAt               time.Time  `json:"queued_at"`
	CreatedAt              time.Time  `json:"created_at"`
	AssignedAt             *time.Time `json:"assigned_at,omitempty"`
	DissolvedAt            *time.Time `json:"dissolved_at,omitempty"`
}
