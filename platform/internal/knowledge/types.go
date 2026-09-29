// Package knowledge stores reviewed facts and owner-private memos. Stored text
// is untrusted evidence; it never grants application permissions or identity.
package knowledge

import (
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const (
	MaxText         = 2000
	MaxMemoText     = 512
	MaxMemos        = 8
	MaxResults      = 20
	MaxPending      = 10
	Curate          = "curate"
	RemoveFact      = "remove_fact"
	Suggest         = "suggest"
	Review          = "review"
	MemoSet         = "memo_set"
	MemoDelete      = "memo_delete"
	DocumentSet     = "document_set"
	DocumentDelete  = "document_delete"
	MaxDocumentText = 16000
	MaxDocuments    = 128
	assess          = "assess"
	pendingFilter   = "pending_filter"
	pendingReview   = "pending_review"
	approve         = "approve"
)

type Service struct{ DB *pgxpool.Pool }

type Scope struct {
	Event     string `json:"event"`
	Phase     string `json:"phase"`
	CanCurate bool   `json:"can_curate"`
	CanReview bool   `json:"can_review"`
}

// Command uses an empty Event for general knowledge. Actor is never a field.
type Command struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Event      string `json:"event"`
	Topic      string `json:"topic"`
	FactKey    string `json:"fact_key"`
	Text       string `json:"text"`
	Version    int64  `json:"version"`
	ProposalID int64  `json:"proposal_id"`
	Decision   string `json:"decision"`
}

// Assessment is used only by the trusted host classification workflow, never a
// public API or an agent tool. A positive verdict leaves the proposal private until the author submits it.
type Assessment struct {
	Key        string
	ProposalID int64
	Version    int64
	Worthwhile bool
	Reason     string
}

type Fact struct {
	ReadAuthorities    []readsource.Authority `json:"read_authorities,omitempty"`
	Event              string                 `json:"event"`
	Topic              string                 `json:"topic"`
	Key                string                 `json:"key"`
	Text               string                 `json:"text"`
	Version            int64                  `json:"version"`
	Phase              string                 `json:"phase"`
	HistoricalFallback bool                   `json:"historical_fallback"`
	Untrusted          bool                   `json:"untrusted"`
	Active             bool                   `json:"active"`
}

type Proposal struct {
	Submitted       bool                   `json:"submitted"`
	ReadAuthorities []readsource.Authority `json:"read_authorities,omitempty"`
	ID              int64                  `json:"id"`
	Event           string                 `json:"event"`
	Owner           string                 `json:"owner"`
	Topic           string                 `json:"topic"`
	FactKey         string                 `json:"fact_key"`
	Text            string                 `json:"text"`
	Version         int64                  `json:"version"`
	FactVersion     int64                  `json:"fact_version"`
	State           string                 `json:"state"`
	Reason          string                 `json:"reason"`
	CreatedAt       time.Time              `json:"created_at"`
}

type Memo struct {
	ReadAuthorities []readsource.Authority `json:"read_authorities,omitempty"`
	Key             string                 `json:"key"`
	Text            string                 `json:"text"`
	Version         int64                  `json:"version"`
	Active          bool                   `json:"active"`
}

type Document struct {
	ReadAuthorities []readsource.Authority `json:"read_authorities,omitempty"`
	Topic           string                 `json:"topic"`
	Key             string                 `json:"key"`
	Text            string                 `json:"text"`
	Version         int64                  `json:"version"`
	Active          bool                   `json:"active"`
}

type Result struct {
	PrivateDeletion *PrivateDeletionWitness `json:"private_deletion,omitempty"`
	ReadAuthorities []readsource.Authority  `json:"read_authorities,omitempty"`
	Redacted        bool                    `json:"redacted,omitempty"`
	Document        *Document               `json:"document,omitempty"`
	Fact            *Fact                   `json:"fact,omitempty"`
	Proposal        *Proposal               `json:"proposal,omitempty"`
	Memo            *Memo                   `json:"memo,omitempty"`
}

type Query struct{ Event, Topic, Text, Cursor string }
type FactPage struct {
	ReadAuthorities []readsource.Authority `json:"read_authorities,omitempty"`
	Facts           []Fact                 `json:"facts"`
	More            bool                   `json:"more"`
	NextCursor      string                 `json:"next_cursor"`
}
type ProposalQuery struct {
	Event       string
	ReviewQueue bool
	After       int64
}

func invalid() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: "knowledge_invalid"}
}
func forbidden() error { return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"} }
func missing() error {
	return &core.ProblemError{Status: http.StatusNotFound, Code: "knowledge_not_found"}
}
func conflict(code string) error { return &core.ProblemError{Status: http.StatusConflict, Code: code} }

// MemoPage serializes collection evidence once, outside the original content budget.
type MemoPage struct {
	Items           []Memo                 `json:"items"`
	ReadAuthorities []readsource.Authority `json:"read_authorities"`
}
type ProposalPage struct {
	Items           []Proposal             `json:"items"`
	ReadAuthorities []readsource.Authority `json:"read_authorities"`
}

// RestoreReadAuthorities projects one wire envelope back onto its typed bodies
// for host callers that consume an individual result field.
func (r *Result) RestoreReadAuthorities() {
	if r.Fact != nil {
		r.Fact.ReadAuthorities = readsource.CloneAuthorities(r.ReadAuthorities)
	}
	if r.Memo != nil {
		r.Memo.ReadAuthorities = readsource.CloneAuthorities(r.ReadAuthorities)
	}
	if r.Document != nil {
		r.Document.ReadAuthorities = readsource.CloneAuthorities(r.ReadAuthorities)
	}
	if r.Proposal != nil {
		r.Proposal.ReadAuthorities = readsource.CloneAuthorities(r.ReadAuthorities)
	}
}
