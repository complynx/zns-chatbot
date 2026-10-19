package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type miniAppPairWrite[A any] struct {
	OperationID string `json:"operation_id"`
	Arguments   A      `json:"arguments"`
}

func (s *Server) registerMiniAppPairs() {
	s.public.HandleFunc("GET /app/pairs/my", s.miniAppPairsRead)
	s.public.HandleFunc("POST /app/pairs/dissolve", s.miniAppPairDissolve)
	s.public.HandleFunc("GET /app/pairs/events", s.miniAppPairsRead)
	s.public.HandleFunc("GET /app/pairs/invitations", s.miniAppPairsRead)
	s.public.HandleFunc("GET /app/pairs/events/{event}/context", s.miniAppPairsRead)
	s.public.HandleFunc("GET /app/pairs/operations/{operation}", s.miniAppPairsRead)
	s.public.HandleFunc("POST /app/pairs/invite", s.miniAppPairInvite)
	for _, decision := range []string{"accept", "decline", "revoke"} {
		s.public.HandleFunc(
			"POST /app/pairs/"+decision,
			func(w http.ResponseWriter, r *http.Request) { s.miniAppPairDecide(w, r, decision) },
		)
	}
}

func miniAppPairScope(attempt db.MiniAppAttempt) db.PaymentScope {
	u := attempt.User
	return db.PaymentScope{
		BotID: u.BotID, Organization: u.Organization,
		UserID:  u.ID,
		Subject: u.Subject,
	}
}

func miniAppPairID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return uuid.Nil, registrations.ErrInvalid
	}
	return id, nil
}

func (s *Server) miniAppPairsRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	scope := miniAppPairScope(attempt)
	query := r.URL.Query()
	var result any
	switch {
	case r.PathValue("event") != "":
		var event uuid.UUID
		event, err = miniAppPairID(r.PathValue("event"))
		if err == nil {
			result, err = s.store.PairInvitationContext(ctx, scope, event)
		}
	case r.PathValue("operation") != "":
		if query.Get("kind") == "dissolve" {
			result, err = s.store.PairDissolutionOperation(ctx, scope, r.PathValue("operation"))
		} else {
			result, err = s.store.PairInvitationOperation(ctx, scope, r.PathValue("operation"), query.Get("kind"))
		}
	default:
		var after uuid.UUID
		if raw := query.Get("after"); raw != "" {
			after, err = miniAppPairID(raw)
		}
		if err != nil {
			break
		}
		switch r.URL.Path {
		case "/app/pairs/events":
			result, err = s.store.ListEventContexts(
				ctx,
				db.EventContextScope{EventScope: scope.EventScope, UserID: scope.UserID, Subject: scope.Subject},
				"registrations",
				after,
				events.ContextChoiceLimit,
			)
		case "/app/pairs/my":
			result, err = s.miniAppOwnedPairs(ctx, scope, query, after)
		default:
			result, err = s.miniAppPairList(ctx, scope, query, after)
		}
	}

	s.miniAppPairResult(ctx, w, r, result, err)
}

func (s *Server) miniAppPairList(
	ctx context.Context,
	scope db.PaymentScope,
	query url.Values,
	after uuid.UUID,
) (pairInvitationListResult, error) {
	args := pairInvitationListArguments{Direction: query.Get("direction"), State: query.Get("state"), After: after}
	if raw := query.Get("event_id"); raw != "" {
		id, err := miniAppPairID(raw)
		if err != nil {
			return pairInvitationListResult{}, err
		}
		args.EventID = id
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || limit < 1 || limit > int64(agentTaskPageLimit) {
			return pairInvitationListResult{}, registrations.ErrInvalid
		}
		args.Limit = int32(limit)
	}
	args = normalizedPairInvitationList(args)
	rows, err := s.store.PairInvitations(
		ctx,
		scope,
		db.PairInvitationList{
			Direction: args.Direction,
			State:     args.State,
			EventID:   args.EventID,
			After:     args.After,
			Limit:     args.Limit,
		},
	)
	page := pairInvitationListResult{Invitations: rows}
	if len(rows) == int(args.Limit) {
		page.Next = &rows[len(rows)-1].Invitation.ID
	}
	return page, err
}

func decodeMiniAppPair(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid pair invitation request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid pair invitation request")
		return false
	}
	return true
}

func (s *Server) miniAppPairCredentials(
	ctx context.Context,
	r *http.Request,
) (db.PaymentScope, db.MiniAppPairSession, error) {
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		return db.PaymentScope{}, db.MiniAppPairSession{}, err
	}
	bearer, err := miniAppBearer(r)
	return miniAppPairScope(attempt), db.MiniAppPairSession{Bearer: bearer, Generation: s.miniAppKey.Generation()}, err
}

func (s *Server) miniAppPairInvite(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPairWrite[db.PairInvitationRequest]
	if !decodeMiniAppPair(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPairCredentials(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.CreateMiniAppPairInvitation(ctx, scope, session, input.OperationID, input.Arguments)
	s.miniAppPairResult(ctx, w, r, result, err)
}

func (s *Server) miniAppPairDecide(w http.ResponseWriter, r *http.Request, decision string) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPairWrite[db.PairInvitationDecision]
	if !decodeMiniAppPair(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPairCredentials(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.DecideMiniAppPairInvitation(
		ctx,
		scope,
		session,
		input.OperationID,
		decision,
		input.Arguments,
	)
	s.miniAppPairResult(ctx, w, r, result, err)
}

func (s *Server) miniAppPairResult(ctx context.Context, w http.ResponseWriter, r *http.Request, result any, err error) {
	if err == nil {
		_, err = s.currentMiniApp(ctx, r)
	}
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	case errors.Is(err, db.ErrMiniAppDenied):
		miniAppError(w, err)
	case errors.Is(err, db.ErrPairDenied):
		miniAppHTTPError(w, http.StatusForbidden, "pair participant access required")
	case errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusNotFound, "pair invitation or current access not found")
	case errors.Is(err, registrations.ErrInvalid):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid pair invitation request")
	case errors.Is(err, db.ErrConflict),
		errors.Is(err, registrations.ErrUnavailable),
		errors.Is(err, registrationdetails.ErrRequired):
		miniAppHTTPError(w, http.StatusConflict, "invitation or registration requirements changed; read current state")
	default:
		w.Header().Set("Retry-After", "1")
		miniAppHTTPError(
			w,
			http.StatusServiceUnavailable,
			"pair invitation temporarily unavailable; retry the original operation",
		)
	}
}

func (s *Server) miniAppOwnedPairs(
	ctx context.Context,
	scope db.PaymentScope,
	query url.Values,
	after uuid.UUID,
) (ownedPairListResult, error) {
	args := ownedPairArguments{After: after}
	if raw := query.Get("event_id"); raw != "" {
		id, err := miniAppPairID(raw)
		if err != nil {
			return ownedPairListResult{}, err
		}
		args.EventID = id
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || limit < 1 || limit > int64(agentTaskPageLimit) {
			return ownedPairListResult{}, registrations.ErrInvalid
		}
		args.Limit = int32(limit)
	}
	return s.ownedPairPage(ctx, scope, args)
}

func (s *Server) miniAppPairDissolve(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppPairWrite[db.PairDissolution]
	if !decodeMiniAppPair(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPairCredentials(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.DissolveMiniAppPair(ctx, scope, session, input.OperationID, input.Arguments)
	s.miniAppPairResult(ctx, w, r, result, err)
}
