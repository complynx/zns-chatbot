package db

import "github.com/google/uuid"

// recoveryCheckpoint is the storage view of durable task metadata used by
// ownership, closure and onboarding. JSON tags are a persisted protocol: keep
// old snapshots readable when the task writer changes; ignore unrelated fields.
type recoveryCheckpoint struct {
	Reauthorizing bool                  `json:"reauthorizing"`
	Origins       []uuid.UUID           `json:"recovery_start_origins"`
	Welcome       bool                  `json:"welcome_after_onboarding"`
	Run           recoveryRunState      `json:"run"`
	Recovery      *recoveryMessageState `json:"recovery"`
}

type recoveryRunState struct {
	ID       string `json:"run_id"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Replaces string `json:"replaces_run_id"`
}

type recoveryMessageState struct {
	Rejected   bool   `json:"rejected"`
	Observed   string `json:"observed_run_id"`
	Superseded string `json:"superseded_run_id"`
	Receipt    *struct {
		MessageID string `json:"message_id"`
		Delivered bool   `json:"delivered"`
		Abandoned bool   `json:"abandoned"`
	} `json:"receipt"`
}
