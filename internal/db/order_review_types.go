package db

import (
	"time"

	"github.com/google/uuid"
)

// OrderReviewWorkspace retains one exact actor-owned decision revision.
type OrderReviewWorkspace struct {
	ID          uuid.UUID                      `json:"id"`
	EventID     uuid.UUID                      `json:"event_id"`
	EvidenceID  uuid.UUID                      `json:"evidence_id"`
	Revision    int64                          `json:"revision"`
	State       string                         `json:"state"`
	Current     bool                           `json:"current"`
	Preparation OrderEvidenceReviewPreparation `json:"preparation"`
}

type OrderReviewDraftChange struct {
	Language         string              `json:"language,omitempty" enum:"auto,en,ru"`
	Review           OrderEvidenceReview `json:"review"`
	ExpectedRevision int64               `json:"expected_revision"`
}

type OrderReviewWorkspaceChange struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Revision    int64     `json:"revision"`
}

// OrderReviewCommitChange repeats the human-visible decision, not just its ID.
type OrderReviewCommitChange struct {
	WorkspaceID uuid.UUID           `json:"workspace_id"`
	Revision    int64               `json:"revision"`
	Review      OrderEvidenceReview `json:"review"`
}

type OrderReviewCommitPreparation struct {
	Description string                  `json:"description"`
	ID          uuid.UUID               `json:"proposal_id"`
	Arguments   OrderReviewCommitChange `json:"arguments"`
	ExpiresAt   time.Time               `json:"expires_at"`
}

type OrderWorkspaceReviewResult struct {
	CanonicalOperationID uuid.UUID            `json:"canonical_operation_id"`
	Evidence             OrderPaymentEvidence `json:"evidence"`
}

// OrderReviewReceipt describes one call's phase. Completed save/cancel calls
// record intent only; they never decide money.
type OrderReviewReceipt struct {
	UnavailableReason string                        `json:"unavailable_reason,omitempty"`
	OperationID       uuid.UUID                     `json:"operation_id"`
	Kind              string                        `json:"kind"`
	Source            string                        `json:"source"`
	Completed         bool                          `json:"completed"`
	Workspace         *OrderReviewWorkspace         `json:"workspace,omitempty"`
	Preparation       *OrderReviewCommitPreparation `json:"preparation,omitempty"`
	Review            *OrderWorkspaceReviewResult   `json:"review,omitempty"`
}
