package db

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/knowledge"
)

var ErrKnowledgeDenied = errors.New("current knowledge authority required")

const (
	kbSuggest        = "suggest"
	kbEdit           = "edit"
	kbReview         = "review"
	kbSetModerator   = "set_moderator"
	kbApprove        = "approve"
	kbReject         = "reject"
	kbPending        = "pending"
	kbOverviewLimit  = 20
	kbTopicLimit     = 40
	kbArchiveBatch   = 20
	kbPreparationTTL = 5 * time.Minute
)

type KnowledgeArticle struct {
	ID              uuid.UUID         `json:"id"`
	Revision        int64             `json:"revision"`
	CurrentRevision int64             `json:"current_revision"`
	Content         knowledge.Content `json:"content"`
	EventID         *uuid.UUID        `json:"event_id,omitempty"`
	EventName       string            `json:"event_name,omitempty"`
	EventStartsAt   *time.Time        `json:"event_starts_at,omitempty"`
	EventEndsAt     *time.Time        `json:"event_ends_at,omitempty"`
	EventTimezone   string            `json:"event_timezone,omitempty"`
	ArchivedAt      *time.Time        `json:"archived_at,omitempty"`
	Historical      bool              `json:"historical"`
	UpdatedAt       time.Time         `json:"updated_at"`
}
type KnowledgeChange struct {
	Kind                     string             `json:"kind"`
	ArticleID                uuid.UUID          `json:"article_id,omitempty"`
	ExpectedRevision         int64              `json:"expected_revision,omitempty"`
	EventID                  *uuid.UUID         `json:"event_id,omitempty"`
	Content                  *knowledge.Content `json:"content,omitempty"`
	SuggestionID             uuid.UUID          `json:"suggestion_id,omitempty"`
	Decision                 string             `json:"decision,omitempty"`
	Reason                   string             `json:"reason"`
	ModeratorUserID          uuid.UUID          `json:"moderator_user_id,omitempty"`
	ModeratorEnabled         *bool              `json:"moderator_enabled,omitempty"`
	ExpectedModeratorVersion int64              `json:"expected_moderator_version,omitempty"`
}
type KnowledgePreparation struct {
	ID          uuid.UUID       `json:"proposal_id"`
	Arguments   KnowledgeChange `json:"arguments"`
	Description string          `json:"description"`
	ExpiresAt   time.Time       `json:"expires_at"`
}
type KnowledgeModerator struct {
	UserID    uuid.UUID `json:"user_id"`
	Enabled   bool      `json:"enabled"`
	Version   int64     `json:"version"`
	ChangedBy uuid.UUID `json:"changed_by"`
	ChangedAt time.Time `json:"changed_at"`
}
type KnowledgeModeratorPage struct {
	Items []KnowledgeModerator `json:"items"`
	Next  *uuid.UUID           `json:"next,omitempty"`
}
type KnowledgeSuggestion struct {
	ID                uuid.UUID         `json:"id"`
	ArticleID         uuid.UUID         `json:"article_id"`
	BaseRevision      int64             `json:"base_revision"`
	EventID           *uuid.UUID        `json:"event_id,omitempty"`
	EventVersion      int64             `json:"event_version,omitempty"`
	ProposerID        uuid.UUID         `json:"proposer_id"`
	Reason            string            `json:"reason"`
	Content           knowledge.Content `json:"content"`
	State             string            `json:"state"`
	ReviewerID        *uuid.UUID        `json:"reviewer_id,omitempty"`
	DecisionReason    string            `json:"decision_reason,omitempty"`
	PublishedRevision int64             `json:"published_revision,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	DecidedAt         *time.Time        `json:"decided_at,omitempty"`
}
type KnowledgeResult struct {
	Kind       string               `json:"kind"`
	Article    *KnowledgeArticle    `json:"article,omitempty"`
	Suggestion *KnowledgeSuggestion `json:"suggestion,omitempty"`
	Moderator  *KnowledgeModerator  `json:"moderator,omitempty"`
}
type KnowledgeListRequest struct {
	Collection knowledge.Collection `json:"collection"`
	EventID    *uuid.UUID           `json:"event_id,omitempty"`
	Language   string               `json:"language,omitempty"`
	Topic      string               `json:"topic,omitempty"`
	After      uuid.UUID            `json:"after,omitempty"`
	Limit      int                  `json:"limit,omitempty"`
}
type KnowledgeArticlePage struct {
	Items []KnowledgeArticle `json:"items"`
	Next  *uuid.UUID         `json:"next,omitempty"`
}
type KnowledgeSearchHit struct {
	Article        KnowledgeArticle `json:"article"`
	MatchedQueries []string         `json:"matched_queries"`
}
type KnowledgeSearchPage struct {
	Items []KnowledgeSearchHit `json:"items"`
	Next  string               `json:"next,omitempty"`
}
type KnowledgeRevision struct {
	Revision         int64      `json:"revision"`
	PreviousRevision int64      `json:"previous_revision,omitempty"`
	ChangeKind       string     `json:"change_kind"`
	AuthorID         *uuid.UUID `json:"author_id,omitempty"`
	ProposerID       *uuid.UUID `json:"proposer_id,omitempty"`
	ReviewerID       *uuid.UUID `json:"reviewer_id,omitempty"`
	Reason           string     `json:"reason"`
	CreatedAt        time.Time  `json:"created_at"`
}
type KnowledgeRevisionPage struct {
	Items []KnowledgeRevision `json:"items"`
	Next  *int64              `json:"next,omitempty"`
}
type KnowledgeSuggestionRequest struct {
	Pending bool       `json:"pending"`
	EventID *uuid.UUID `json:"event_id,omitempty"`
	After   uuid.UUID  `json:"after,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}
type KnowledgeSuggestionPage struct {
	Items []KnowledgeSuggestion `json:"items"`
	Next  *uuid.UUID            `json:"next,omitempty"`
}
type KnowledgeCapabilities struct {
	Moderator           bool `json:"moderator"`
	EventAdministrator  bool `json:"event_administrator"`
	GlobalAdministrator bool `json:"global_administrator"`
	CanEdit             bool `json:"can_edit"`
}
type KnowledgeOverviewItem struct {
	Collection knowledge.Collection `json:"collection"`
	EventID    *uuid.UUID           `json:"event_id,omitempty"`
	EventName  string               `json:"event_name,omitempty"`
	Count      int64                `json:"count"`
}
type KnowledgeTopic struct {
	Topic string `json:"topic"`
	Count int64  `json:"count"`
}
type KnowledgeOverview struct {
	Items     []KnowledgeOverviewItem `json:"items"`
	Topics    []KnowledgeTopic        `json:"topics"`
	Truncated bool                    `json:"truncated"`
}
