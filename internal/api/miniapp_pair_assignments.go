package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type miniAppPairAssignmentInput struct {
	OperationID      string            `json:"operation_id"`
	Arguments        db.PairAssignment `json:"arguments"`
	ResumeProposalID json.RawMessage   `json:"resume_proposal_id,omitempty"`
	ProposalID       json.RawMessage   `json:"proposal_id,omitempty"`
	Confirmed        json.RawMessage   `json:"confirmed,omitempty"`
}

func (s *Server) registerMiniAppPairAssignments() {
	for _, path := range []string{"recent",
		schemaEvents,
		"events/{event}/candidates",
		assignmentProposalsField,
		"proposals/{proposal}",
		"operations/{operation}"} {
		s.public.HandleFunc("GET /app/pair-assignments/"+path, s.miniAppPairAssignmentRead)
	}
	s.public.HandleFunc("POST /app/pair-assignments/prepare", s.miniAppPairAssignmentWrite)
	s.public.HandleFunc("POST /app/pair-assignments/confirm", s.miniAppPairAssignmentWrite)
}

func (s *Server) miniAppPairAssignmentSession(
	ctx context.Context, r *http.Request,
) (db.PaymentScope, db.MiniAppPairAssignmentSession, error) {
	scope := db.PaymentScope{EventScope: s.eventScope()}
	var session db.MiniAppPairAssignmentSession
	attempt, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return scope, session, err
	}
	unchanged := s.iam.InvalidationFence(attempt.User.Subject)
	principal, err := s.miniAppVerifiedPrincipal(ctx, attempt.User)
	if err != nil {
		return scope, session, err
	}
	scope.UserID, scope.Subject = attempt.User.ID, principal.Subject
	_, scope.GlobalAdmin = principal.Roles["zinusya.admin"][scope.Organization]
	if !unchanged() || !time.Now().Before(principal.ExpiresAt) {
		return scope, session, db.ErrPairDenied
	}
	allowed, err := s.store.HasPairAssignmentAccess(ctx, scope)
	if err != nil {
		return scope, session, err
	}
	if !allowed {
		return scope, session, db.ErrPairDenied
	}
	session.UserID = attempt.User.ID
	session.Bearer, err = miniAppBearer(r)
	session.Generation = s.miniAppKey.Generation()
	session.AuthorityUnchanged = func() bool { return unchanged() && time.Now().Before(principal.ExpiresAt) }
	return scope, session, err
}

func miniAppPairAssignmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrPairDenied), errors.Is(err, payments.ErrDenied):
		miniAppHTTPError(w, http.StatusForbidden, "current assignment administrator required")
	case errors.Is(err, registrations.ErrInvalid):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid pair assignment request")
	case errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusNotFound, "event, pair or actor-owned proposal unavailable")
	case errors.Is(err, db.ErrConflict),
		errors.Is(err, registrations.ErrCapacity),
		errors.Is(err, registrations.ErrUnavailable),
		errors.Is(err, registrationdetails.ErrRequired):
		miniAppHTTPError(
			w,
			http.StatusConflict,
			"assignment terms, eligibility or capacity changed; check the original operation",
		)
	default:
		miniAppError(w, err)
	}
}

func (s *Server) miniAppPairAssignmentResult(ctx context.Context, w http.ResponseWriter, r *http.Request,
	scope db.PaymentScope, user uuid.UUID, result any, err error,
) {
	if err == nil {
		current, session, e := s.miniAppPairAssignmentSession(ctx, r)
		err = e
		if err == nil && (current != scope || session.UserID != user) {
			err = db.ErrMiniAppDenied
		}
	}
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) miniAppPairAssignmentWrite(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPairAssignmentInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		miniAppPairAssignmentError(w, registrations.ErrInvalid)
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppPairAssignmentError(w, registrations.ErrInvalid)
		return
	}
	prepare := strings.HasSuffix(r.URL.Path, "/prepare")
	if input.OperationID == "" || len(input.OperationID) > 128 ||
		(prepare && (len(input.Confirmed) != 0 || len(input.ProposalID) != 0)) ||
		(!prepare && len(input.ResumeProposalID) != 0) {
		miniAppPairAssignmentError(w, registrations.ErrInvalid)
		return
	}
	var proposal, resume uuid.UUID
	if prepare && len(input.ResumeProposalID) != 0 {
		if json.Unmarshal(input.ResumeProposalID, &resume) != nil || resume == uuid.Nil {
			miniAppPairAssignmentError(w, registrations.ErrInvalid)
			return
		}
	}
	if !prepare {
		var confirmed bool
		if json.Unmarshal(input.Confirmed, &confirmed) != nil || !confirmed ||
			json.Unmarshal(input.ProposalID, &proposal) != nil || proposal == uuid.Nil {
			miniAppPairAssignmentError(w, registrations.ErrInvalid)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPairAssignmentSession(ctx, r)
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	var result any
	if prepare {
		result, err = s.store.PrepareMiniAppPairAssignment(
			ctx,
			scope,
			input.OperationID,
			input.Arguments,
			resume,
			session,
		)
	} else {
		result, err = s.store.ConfirmMiniAppPairAssignment(
			ctx,
			scope,
			input.OperationID,
			input.Arguments,
			proposal,
			session,
		)
	}
	s.miniAppPairAssignmentResult(ctx, w, r, scope, session.UserID, result, err)
}

func (s *Server) miniAppPairAssignmentRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPairAssignmentSession(ctx, r)
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	result, err := s.miniAppPairAssignmentLookup(ctx, r, scope, session)
	s.miniAppPairAssignmentResult(ctx, w, r, scope, session.UserID, result, err)
}

func (s *Server) miniAppPairAssignmentLookup(ctx context.Context, r *http.Request, scope db.PaymentScope,
	session db.MiniAppPairAssignmentSession,
) (any, error) {
	if operation := r.PathValue("operation"); operation != "" {
		return s.store.MiniAppPairAssignmentProposal(ctx, scope, session, uuid.Nil, operation)
	}
	if raw := r.PathValue("proposal"); raw != "" {
		proposal, err := miniAppPairID(raw)
		if err != nil {
			return nil, err
		}
		return s.store.MiniAppPairAssignmentProposal(ctx, scope, session, proposal, "")
	}
	after := uuid.Nil
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = miniAppPairID(raw)
		if err != nil {
			return nil, err
		}
	}
	if raw := r.PathValue("event"); raw != "" {
		event, err := miniAppPairID(raw)
		if err != nil {
			return nil, err
		}
		return s.store.MiniAppPairAssignmentCandidates(ctx, scope, session, event, after)
	}
	if strings.HasSuffix(r.URL.Path, "/recent") {
		return s.store.RecentMiniAppPairAssignmentProposals(ctx, scope, session)
	}
	if strings.HasSuffix(r.URL.Path, "/events") {
		return s.store.MiniAppPairAssignmentEvents(ctx, scope, session, after)
	}
	return s.store.PendingMiniAppPairAssignmentProposals(ctx, scope, session, after)
}
