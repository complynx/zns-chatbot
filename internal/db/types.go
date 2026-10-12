package db

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrConflict       = errors.New("state version conflict")
	ErrLeaseLost      = errors.New("task lease lost")
	ErrRoutingPending = errors.New("earlier inbox routing pending")
	ErrNoActiveTask   = errors.New("conversation has no active task")
)

type IntakeRequest struct {
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
type Settings struct {
	AuthCacheTTLSeconds    int    `json:"auth_cache_ttl_seconds"`
	DefaultLanguage        string `json:"default_language"`
	MaxParallelTasks       int    `json:"max_parallel_tasks"`
	QuestionTimeoutSeconds int    `json:"question_timeout_seconds"`
}
type VersionedSettings struct {
	Version  int64    `json:"version"`
	Document Settings `json:"document"`
}
