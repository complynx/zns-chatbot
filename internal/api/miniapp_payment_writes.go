package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

// Pointers require explicit zero-valued money/version preconditions in JSON.
type miniAppPaymentReviewInput struct {
	EventID         uuid.UUID `json:"event_id"`
	EvidenceID      uuid.UUID `json:"evidence_id"`
	ExpectedVersion *int64    `json:"expected_version"`
	Decision        string    `json:"decision"`
	AmountMinor     *int64    `json:"amount_minor"`
	Reason          string    `json:"reason"`
}

func (input miniAppPaymentReviewInput) change() (payments.ReviewChange, error) {
	if input.ExpectedVersion == nil || input.AmountMinor == nil {
		return payments.ReviewChange{}, payments.ErrInvalid
	}
	return (payments.ReviewChange{EventID: input.EventID, EvidenceID: input.EvidenceID,
		ExpectedVersion: *input.ExpectedVersion, Decision: input.Decision, AmountMinor: *input.AmountMinor, Reason: input.Reason}).Normalize()
}

type miniAppPaymentSaveInput struct {
	OperationID      uuid.UUID                 `json:"operation_id"`
	ExpectedRevision *int64                    `json:"expected_revision"`
	Review           miniAppPaymentReviewInput `json:"review"`
}

type miniAppPaymentCancelInput struct {
	OperationID uuid.UUID `json:"operation_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Revision    int64     `json:"revision"`
}

type miniAppPaymentCommitInput struct {
	OperationID         uuid.UUID `json:"operation_id"`
	CanonicalProposalID uuid.UUID `json:"canonical_proposal_id"`
	Confirmed           bool      `json:"confirmed"`
	Arguments           struct {
		WorkspaceID uuid.UUID                 `json:"workspace_id"`
		Revision    int64                     `json:"revision"`
		Review      miniAppPaymentReviewInput `json:"review"`
	} `json:"arguments"`
}

func (s *Server) miniAppPaymentSave(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPaymentSaveInput
	if !decodeMiniAppPayment(w, r, &input) {
		return
	}
	review, err := input.Review.change()
	if err != nil || input.OperationID == uuid.Nil || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
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
	result, err := s.store.PrepareMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		input.OperationID.String(),
		db.PaymentReviewDraftChange{Review: review, ExpectedRevision: *input.ExpectedRevision},
	)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}

func (s *Server) miniAppPaymentCancel(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPaymentCancelInput
	if !decodeMiniAppPayment(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil || input.WorkspaceID == uuid.Nil || input.Revision < 1 {
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
	result, err := s.store.CancelMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		input.OperationID.String(),
		db.PaymentReviewWorkspaceChange{WorkspaceID: input.WorkspaceID, Revision: input.Revision},
	)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}

func (s *Server) miniAppPaymentConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPaymentCommitInput
	if !decodeMiniAppPayment(w, r, &input) {
		return
	}
	review, err := input.Arguments.Review.change()
	if err != nil || !input.Confirmed || input.OperationID == uuid.Nil || input.CanonicalProposalID == uuid.Nil ||
		input.Arguments.WorkspaceID == uuid.Nil ||
		input.Arguments.Revision < 1 {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"explicit confirmation, operation, proposal and exact review required",
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
	result, err := s.store.CommitMiniAppPaymentReviewWorkspace(
		ctx,
		scope,
		session,
		input.OperationID.String(),
		db.MiniAppPaymentReviewCommit{
			Arguments: db.PaymentReviewCommitChange{
				WorkspaceID: input.Arguments.WorkspaceID,
				Revision:    input.Arguments.Revision,
				Review:      review,
			},
			CanonicalProposalID: input.CanonicalProposalID,
		},
	)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}
