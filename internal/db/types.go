package db

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/files"
)

var (
	ErrConflict       = errors.New("state version conflict")
	ErrLeaseLost      = errors.New("task lease lost")
	ErrRoutingPending = errors.New("earlier inbox routing pending")
	ErrNoActiveTask   = errors.New("conversation has no active task")
)

type IntakeRequest struct {
	Attachments                             []files.Incoming
	BotID, UpdateID, TelegramUserID, ChatID int64
	Profile, Payload                        json.RawMessage
}
type Task struct {
	ID, UserID, ConversationID, TargetTaskID                    uuid.UUID
	Seq                                                         int64
	Request                                                     json.RawMessage
	State, Route, TargetRunID, RunID, AuthSessionID, QuestionID string
	Snapshot                                                    json.RawMessage
	Version, Fence                                              int64
	LeaseOwner                                                  string
}
type User struct {
	ID                    uuid.UUID
	BotID, TelegramUserID int64
	Profile               json.RawMessage
	Subject, Organization string
	Revoked               bool
}
type Outbound struct {
	ID       uuid.UUID
	DedupKey string
	ChunkSeq int32
	Payload  json.RawMessage
}
type Delivery struct {
	ID, TaskID uuid.UUID
	DedupKey   string
	ChunkSeq   int32
	Payload    json.RawMessage
	State      string
	AttemptID  uuid.UUID
	Receipt    json.RawMessage
}

// DeliveryReceipt reports an applied receipt. ResponseElapsedSeconds is present
// only for the transaction that first confirms every final-response chunk.
type DeliveryReceipt struct {
	Delivery

	ResponseElapsedSeconds *float64
}
type Settings struct {
	// Global browser avatar original budget in bytes; zero uses 10000000000 (10 GB). Range 1 to 10000000000.
	// Lowering stops admission without evicting unexpired originals.
	AvatarBrowserStorageLimitBytes int64 `json:"avatar_browser_storage_limit_bytes,omitempty"`
	// Browser avatar source lifetime from upload; zero uses 7776000 (90 days). Range 60 to 7776000. Lowering also
	// expires existing sources; completed JPEGs and receipts remain.
	AvatarBrowserRetentionSeconds int `json:"avatar_browser_retention_seconds,omitempty"`
	// Long massage reminder lead in nonnegative 32-bit seconds; zero uses 3600. Changes reschedule pending/retryable
	// reminders.
	MassageNotifyClientPriorLongSeconds int `json:"massage_notify_client_prior_long_seconds,omitempty"`
	// Short massage reminder lead in nonnegative 32-bit seconds; zero uses 600. Changes reschedule pending/retryable
	// reminders.
	MassageNotifyClientPriorSeconds int `json:"massage_notify_client_prior_seconds,omitempty"`
	// Ordinary-owner massage booking limit per configured party window; zero uses the legacy default 3. Range 1 to
	// 100.
	MassageMaxBookingsPerWindow int    `json:"massage_max_bookings_per_window,omitempty"`
	AuthCacheTTLSeconds         int    `json:"auth_cache_ttl_seconds"`
	DefaultLanguage             string `json:"default_language"`
	// Worker concurrency; changes apply after application restart.
	MaxParallelTasks int `json:"max_parallel_tasks"`
	// Reserved for future expectations. Does not change current Loom prompt expiry.
	QuestionTimeoutSeconds int `json:"question_timeout_seconds"`
	// Name request lifetime in seconds; zero uses 7200. Range 60 to 86400.
	NameExpectationSeconds int `json:"name_expectation_seconds,omitempty"`
	// Telegram login proof maximum age; zero uses 300 seconds. Range 30 to 900.
	MiniAppMaxAgeSeconds int `json:"mini_app_max_age_seconds,omitempty"`
	// Login reconciliation window; zero uses 120 seconds. Range 30 to 300.
	MiniAppPendingSeconds int `json:"mini_app_pending_seconds,omitempty"`
	// Absolute browser session lifetime; zero uses 900 seconds. Range 60 to 3600. Existing sessions do not extend.
	MiniAppSessionSeconds int `json:"mini_app_session_seconds,omitempty"`
}
type VersionedSettings struct {
	Version  int64    `json:"version"`
	Document Settings `json:"document"`
}
