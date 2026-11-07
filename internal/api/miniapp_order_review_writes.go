package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

// A pointer requires the evidence revision to be explicit in the browser intent.
// The shared store normalizes and validates the full decision before any write.
type miniAppOrderReviewInput struct {
	EventID         uuid.UUID `json:"event_id"`
	EvidenceID      uuid.UUID `json:"evidence_id"`
	ExpectedVersion *int64    `json:"expected_version"`
	Decision        string    `json:"decision"`
	Reason          string    `json:"reason"`
}

func (in miniAppOrderReviewInput) arguments() (db.OrderEvidenceReview, bool) {
	if in.ExpectedVersion == nil {
		return db.OrderEvidenceReview{}, false
	}
	return db.OrderEvidenceReview{EventID: in.EventID, EvidenceID: in.EvidenceID,
		ExpectedVersion: *in.ExpectedVersion, Decision: in.Decision, Reason: in.Reason}, true
}

type miniAppOrderReviewSaveInput struct {
	OperationID      uuid.UUID               `json:"operation_id"`
	ExpectedRevision *int64                  `json:"expected_revision"`
	Language         string                  `json:"language,omitempty"`
	Review           miniAppOrderReviewInput `json:"review"`
}

type miniAppOrderReviewCancelInput struct {
	OperationID uuid.UUID `json:"operation_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Revision    int64     `json:"revision"`
}

type miniAppOrderReviewConfirmInput struct {
	OperationID         uuid.UUID `json:"operation_id"`
	CanonicalProposalID uuid.UUID `json:"canonical_proposal_id"`
	Confirmed           bool      `json:"confirmed"`
	Arguments           struct {
		WorkspaceID uuid.UUID               `json:"workspace_id"`
		Revision    int64                   `json:"revision"`
		Review      miniAppOrderReviewInput `json:"review"`
	} `json:"arguments"`
}

func (s *Server) miniAppOrderReviewSave(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var in miniAppOrderReviewSaveInput
	if !decodeMiniAppOrders(w, r, &in) {
		return
	}
	review, ok := in.Review.arguments()
	if !ok || in.OperationID == uuid.Nil || in.ExpectedRevision == nil || *in.ExpectedRevision < 0 {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit operation, revision and review parameters required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.PrepareMiniAppOrderReviewWorkspace(ctx, scope, session, in.OperationID,
		db.OrderReviewDraftChange{Language: in.Language, ExpectedRevision: *in.ExpectedRevision, Review: review})
	s.miniAppOrderReviewResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderReviewCancel(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var in miniAppOrderReviewCancelInput
	if !decodeMiniAppOrders(w, r, &in) {
		return
	}
	if in.OperationID == uuid.Nil || in.WorkspaceID == uuid.Nil || in.Revision < 1 {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit operation and workspace revision required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.CancelMiniAppOrderReviewWorkspace(ctx, scope, session, in.OperationID,
		db.OrderReviewWorkspaceChange{WorkspaceID: in.WorkspaceID, Revision: in.Revision})
	s.miniAppOrderReviewResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderReviewConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var in miniAppOrderReviewConfirmInput
	if !decodeMiniAppOrders(w, r, &in) {
		return
	}
	review, ok := in.Arguments.Review.arguments()
	if !ok || !in.Confirmed || in.OperationID == uuid.Nil || in.CanonicalProposalID == uuid.Nil ||
		in.Arguments.WorkspaceID == uuid.Nil ||
		in.Arguments.Revision < 1 {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"explicit confirmation, operation, canonical proposal and exact review required",
		)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.CommitMiniAppOrderReviewWorkspace(
		ctx,
		scope,
		session,
		in.OperationID,
		db.OrderReviewCommitChange{
			WorkspaceID: in.Arguments.WorkspaceID,
			Revision:    in.Arguments.Revision,
			Review:      review,
		},
		in.CanonicalProposalID,
	)
	s.miniAppOrderReviewResult(ctx, w, r, result, err)
}
