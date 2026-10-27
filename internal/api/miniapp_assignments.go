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

type miniAppAssignmentInput struct {
	OperationID      string            `json:"operation_id"`
	Arguments        db.PassAssignment `json:"arguments"`
	ResumeProposalID uuid.UUID         `json:"resume_proposal_id,omitempty"`
	ProposalID       uuid.UUID         `json:"proposal_id,omitempty"`
	Confirmed        bool              `json:"confirmed,omitempty"`
}

func (s *Server) registerMiniAppAssignments() {
	for _, path := range []string{"recent",
		schemaEvents,
		"events/{event}/registrations",
		assignmentProposalsField,
		"proposals/{proposal}",
		"operations/{operation}"} {
		s.public.HandleFunc("GET /app/pass-assignments/"+path, s.miniAppAssignmentRead)
	}
	s.public.HandleFunc("POST /app/pass-assignments/prepare", s.miniAppAssignmentWrite)
	s.public.HandleFunc("POST /app/pass-assignments/confirm", s.miniAppAssignmentWrite)
}

func (s *Server) miniAppAssignmentSession(
	ctx context.Context,
	r *http.Request,
) (db.EventScope, string, db.MiniAppAssignmentSession, error) {
	scope := db.EventScope{BotID: s.options.BotID, Organization: s.options.OrganizationID}
	var session db.MiniAppAssignmentSession
	attempt, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return scope, "", session, err
	}
	unchanged := s.iam.InvalidationFence(attempt.User.Subject)
	principal, err := s.miniAppVerifiedPrincipal(ctx, attempt.User)
	if err != nil {
		return scope, "", session, err
	}
	if _, ok := principal.Roles["zinusya.admin"][scope.Organization]; !ok || !unchanged() {
		return scope, "", session, payments.ErrDenied
	}
	session.UserID = attempt.User.ID
	session.Bearer, err = miniAppBearer(r)
	session.Generation = s.miniAppKey.Generation()
	session.AuthorityUnchanged = func() bool { return unchanged() && time.Now().Before(principal.ExpiresAt) }
	return scope, principal.Subject, session, err
}

func miniAppAssignmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, payments.ErrDenied):
		miniAppHTTPError(w, http.StatusForbidden, "current global administrator required")
	case errors.Is(err, registrations.ErrInvalid):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid pass assignment request")
	case errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusNotFound, "event, registration or actor-owned proposal unavailable")
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

func (s *Server) miniAppAssignmentResult(ctx context.Context, w http.ResponseWriter, r *http.Request,
	scope db.EventScope, actor string, user uuid.UUID, result any, err error,
) {
	if err == nil {
		current, subject, session, e := s.miniAppAssignmentSession(ctx, r)
		err = e
		if err == nil && (current != scope || subject != actor || session.UserID != user) {
			err = db.ErrMiniAppDenied
		}
	}
	if err != nil {
		miniAppAssignmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) miniAppAssignmentWrite(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppAssignmentInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		miniAppAssignmentError(w, registrations.ErrInvalid)
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppAssignmentError(w, registrations.ErrInvalid)
		return
	}
	prepare := strings.HasSuffix(r.URL.Path, "/prepare")
	if input.OperationID == "" || len(input.OperationID) > 128 ||
		(prepare && (input.Confirmed || input.ProposalID != uuid.Nil)) ||
		(!prepare && (!input.Confirmed || input.ProposalID == uuid.Nil || input.ResumeProposalID != uuid.Nil)) {
		miniAppAssignmentError(w, registrations.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, actor, session, err := s.miniAppAssignmentSession(ctx, r)
	if err != nil {
		miniAppAssignmentError(w, err)
		return
	}
	var result any
	if prepare {
		result, err = s.store.PrepareMiniAppAssignment(
			ctx,
			scope,
			actor,
			input.OperationID,
			input.Arguments,
			input.ResumeProposalID,
			session,
		)
	} else {
		result, err = s.store.ConfirmMiniAppAssignment(
			ctx,
			scope,
			actor,
			input.OperationID,
			input.Arguments,
			input.ProposalID,
			session,
		)
	}
	s.miniAppAssignmentResult(ctx, w, r, scope, actor, session.UserID, result, err)
}

func (s *Server) miniAppAssignmentRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, actor, session, err := s.miniAppAssignmentSession(ctx, r)
	if err != nil {
		miniAppAssignmentError(w, err)
		return
	}
	result, err := s.miniAppAssignmentLookup(ctx, r, scope, actor, session)
	s.miniAppAssignmentResult(ctx, w, r, scope, actor, session.UserID, result, err)
}

func (s *Server) miniAppAssignmentLookup(ctx context.Context, r *http.Request, scope db.EventScope,
	actor string, session db.MiniAppAssignmentSession,
) (any, error) {
	if operation := r.PathValue("operation"); operation != "" {
		return s.store.MiniAppAssignmentProposal(ctx, scope, actor, session, uuid.Nil, operation)
	}
	if raw := r.PathValue("proposal"); raw != "" {
		proposal, err := miniAppPairID(raw)
		if err != nil {
			return nil, err
		}
		return s.store.MiniAppAssignmentProposal(ctx, scope, actor, session, proposal, "")
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
		return s.store.MiniAppAssignmentRegistrations(ctx, scope, actor, session, event, after)
	}
	if strings.HasSuffix(r.URL.Path, "/recent") {
		return s.store.RecentMiniAppAssignmentProposals(ctx, scope, actor, session)
	}
	if strings.HasSuffix(r.URL.Path, "/events") {
		return s.store.MiniAppAssignmentEvents(ctx, scope, actor, session, after)
	}
	return s.store.PendingMiniAppAssignmentProposals(ctx, scope, actor, session, after)
}
