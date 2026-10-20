package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

const knowledgePath = "/internal/agent/knowledge/"

// Transport includes JSON escaping of bounded Markdown. Reads expose passages,
// never a page of complete article bodies.
const knowledgeTransportLimit = 1024 * 1024
const knowledgeResultLimit = 1024 * 1024

const (
	knowledgePreviewTopics       = 4
	knowledgePreviewSources      = 2
	knowledgePreviewBytes        = 256
	knowledgePreviewLines        = 3
	knowledgePreviewHeadings     = 3
	knowledgePreviewContextBytes = 32
)

type knowledgeCapabilitiesArguments struct {
	EventID *uuid.UUID `json:"event_id,omitempty"`
}
type knowledgeModeratorListArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int       `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type knowledgeArticleArguments struct {
	ArticleID uuid.UUID `json:"article_id"`
	Revision  int64     `json:"revision,omitempty" minimum:"0"`
}
type knowledgeReadArguments struct {
	knowledgeArticleArguments
	knowledge.ReadRequest
}
type knowledgeOutlineArguments struct {
	knowledgeArticleArguments
	knowledge.OutlineRequest
}
type knowledgeFindArguments struct {
	knowledgeArticleArguments
	knowledge.FindRequest
}
type knowledgeRevisionArguments struct {
	ArticleID uuid.UUID `json:"article_id"`
	Before    int64     `json:"before,omitempty" minimum:"0"`
	Limit     int       `json:"limit,omitempty"  minimum:"1" maximum:"20"`
}
type knowledgeSuggestionArguments struct {
	knowledge.ReadRequest

	SuggestionID uuid.UUID `json:"suggestion_id"`
}
type knowledgeSuggestionListArguments struct {
	EventID *uuid.UUID `json:"event_id,omitempty"`
	After   uuid.UUID  `json:"after,omitempty"`
	Limit   int        `json:"limit,omitempty"    minimum:"1" maximum:"20"`
}
type knowledgeEditArguments struct {
	ArticleID        uuid.UUID         `json:"article_id,omitempty"`
	ExpectedRevision int64             `json:"expected_revision"    minimum:"0"`
	EventID          *uuid.UUID        `json:"event_id,omitempty"`
	Content          knowledge.Content `json:"content"`
	Reason           string            `json:"reason"                           minLength:"1" maxLength:"1000"`
}
type knowledgeReviewArguments struct {
	SuggestionID     uuid.UUID `json:"suggestion_id"`
	ExpectedRevision int64     `json:"expected_revision" minimum:"0"`
	Decision         string    `json:"decision"                      enum:"approve,reject"`
	Reason           string    `json:"reason"                                              minLength:"1" maxLength:"1000"`
}
type knowledgeModeratorArguments struct {
	ModeratorUserID          uuid.UUID `json:"moderator_user_id"`
	ModeratorEnabled         bool      `json:"moderator_enabled"`
	ExpectedModeratorVersion int64     `json:"expected_moderator_version" minimum:"0"`
	Reason                   string    `json:"reason"                                 minLength:"1" maxLength:"1000"`
}

type knowledgeArticleView struct {
	ID                uuid.UUID               `json:"id"`
	Revision          int64                   `json:"revision"`
	CurrentRevision   int64                   `json:"current_revision"`
	OlderRevision     bool                    `json:"older_revision"`
	Slug              string                  `json:"slug"`
	Title             string                  `json:"title"`
	Language          string                  `json:"language"`
	Topics            []string                `json:"topics"`
	Sources           []knowledge.Source      `json:"sources"`
	Collection        knowledge.Collection    `json:"collection"`
	EventID           *uuid.UUID              `json:"event_id,omitempty"`
	EventName         string                  `json:"event_name,omitempty"`
	EventStartsAt     *time.Time              `json:"event_starts_at,omitempty"`
	EventEndsAt       *time.Time              `json:"event_ends_at,omitempty"`
	EventTimezone     string                  `json:"event_timezone,omitempty"`
	ArchivedAt        *time.Time              `json:"archived_at,omitempty"`
	Historical        bool                    `json:"historical"`
	UpdatedAt         time.Time               `json:"updated_at"`
	MetadataTruncated bool                    `json:"metadata_truncated"`
	Excerpt           knowledge.Excerpt       `json:"excerpt"`
	ExcerptKind       string                  `json:"excerpt_kind"`
	ExcerptAnchor     string                  `json:"excerpt_anchor,omitempty"`
	Outline           knowledge.OutlineResult `json:"outline"`
	MatchedQueries    []string                `json:"matched_queries,omitempty"`
}
type knowledgeListView struct {
	Items     []knowledgeArticleView  `json:"items"`
	Next      *uuid.UUID              `json:"next,omitempty"`
	Filters   db.KnowledgeListRequest `json:"filters"`
	Truncated bool                    `json:"truncated"`
}
type knowledgeSearchView struct {
	Items     []knowledgeArticleView  `json:"items"`
	Next      string                  `json:"next,omitempty"`
	Filters   knowledge.SearchRequest `json:"filters"`
	Truncated bool                    `json:"truncated"`
}
type knowledgeFindView struct {
	Article knowledgeArticleView `json:"article"`
	Matches knowledge.FindResult `json:"matches"`
}
type knowledgeSuggestionView struct {
	ID                uuid.UUID                      `json:"id"`
	ArticleID         uuid.UUID                      `json:"article_id"`
	BaseRevision      int64                          `json:"base_revision"`
	EventID           *uuid.UUID                     `json:"event_id,omitempty"`
	EventVersion      int64                          `json:"event_version,omitempty"`
	ProposerID        uuid.UUID                      `json:"proposer_id"`
	Reason            string                         `json:"reason"`
	State             string                         `json:"state"`
	ReviewerID        *uuid.UUID                     `json:"reviewer_id,omitempty"`
	DecisionReason    string                         `json:"decision_reason,omitempty"`
	PublishedRevision int64                          `json:"published_revision,omitempty"`
	CreatedAt         time.Time                      `json:"created_at"`
	DecidedAt         *time.Time                     `json:"decided_at,omitempty"`
	Content           knowledgeSuggestionContentView `json:"content"`
}

// A suggestion's bytes belong to suggestion_id, never to its target article's
// published base_revision. Keep these references distinct in tool responses.
type knowledgeSuggestionContentView struct {
	Slug              string                  `json:"slug"`
	Title             string                  `json:"title"`
	Language          string                  `json:"language"`
	Topics            []string                `json:"topics"`
	Sources           []knowledge.Source      `json:"sources"`
	MetadataTruncated bool                    `json:"metadata_truncated"`
	Excerpt           knowledge.Excerpt       `json:"excerpt"`
	Outline           knowledge.OutlineResult `json:"outline"`
}
type knowledgeSuggestionListView struct {
	Items     []knowledgeSuggestionView     `json:"items"`
	Next      *uuid.UUID                    `json:"next,omitempty"`
	Filters   db.KnowledgeSuggestionRequest `json:"filters"`
	Truncated bool                          `json:"truncated"`
}
type knowledgeResultView struct {
	Kind       string                   `json:"kind"`
	Article    *knowledgeArticleView    `json:"article,omitempty"`
	Suggestion *knowledgeSuggestionView `json:"suggestion,omitempty"`
	Moderator  *db.KnowledgeModerator   `json:"moderator,omitempty"`
}
type knowledgePreparationResponse[T any] struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     T                       `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}
