package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const avatarAdminDeliveryField = "delivery"

type miniAppAvatarResolutionInput struct {
	OperationID uuid.UUID                   `json:"operation_id"`
	ProposalID  uuid.UUID                   `json:"proposal_id,omitempty"`
	Arguments   db.AvatarDeliveryResolution `json:"arguments"`
	Confirmed   bool                        `json:"confirmed,omitempty"`
}

func (s *Server) registerMiniAppAvatarAdmin() {
	s.public.HandleFunc("GET /app/avatar-admin/deliveries/{delivery}", s.miniAppAvatarAdminRead)
	s.public.HandleFunc("GET /app/avatar-admin/pending", s.miniAppAvatarAdminRead)
	s.public.HandleFunc("GET /app/avatar-admin/proposals/{proposal}", s.miniAppAvatarAdminRead)
	s.public.HandleFunc("GET /app/avatar-admin/operations/{operation}", s.miniAppAvatarAdminRead)
	s.public.HandleFunc("POST /app/avatar-admin/prepare", s.miniAppAvatarAdminWrite)
	s.public.HandleFunc("POST /app/avatar-admin/confirm", s.miniAppAvatarAdminWrite)
}

func (s *Server) miniAppAvatarAdminSession(
	ctx context.Context,
	r *http.Request,
) (db.AvatarDeliveryAdminScope, db.MiniAppAvatarAdminSession, error) {
	var scope db.AvatarDeliveryAdminScope
	var session db.MiniAppAvatarAdminSession
	attempt, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return scope, session, err
	}
	unchanged := s.iam.InvalidationFence(attempt.User.Subject)
	principal, err := s.miniAppVerifiedPrincipal(ctx, attempt.User)
	if err != nil {
		return scope, session, err
	}
	if _, admin := principal.Roles["zinusya.admin"][s.options.OrganizationID]; !admin {
		return scope, session, payments.ErrDenied
	}
	if !unchanged() {
		return scope, session, payments.ErrDenied
	}
	session.AuthorityUnchanged = func() bool { return unchanged() && time.Now().Before(principal.ExpiresAt) }
	session.Bearer, err = miniAppBearer(r)
	session.Generation = s.miniAppKey.Generation()
	profile := db.ProfileScope{
		BotID:        s.options.BotID,
		Organization: s.options.OrganizationID,
		Subject:      principal.Subject,
		UserID:       attempt.User.ID,
	}
	scope = db.AvatarDeliveryAdminScope{ProfileScope: profile, GlobalAdmin: true}
	return scope, session, err
}

func miniAppAvatarAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, payments.ErrDenied):
		miniAppHTTPError(w, http.StatusForbidden, "current global administrator required")
	case errors.Is(err, db.ErrInvalidAvatar):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar delivery proposal or operation")
	case errors.Is(err, db.ErrConflict):
		miniAppHTTPError(
			w,
			http.StatusConflict,
			"avatar delivery or original proposal changed, completed or expired; reconcile the same operation",
		)
	case errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusNotFound, "avatar delivery or actor-owned proposal unavailable")
	default:
		miniAppError(w, err)
	}
}

func (s *Server) miniAppAvatarAdminResult(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	scope db.AvatarDeliveryAdminScope,
	result any,
	err error,
) {
	if err != nil {
		miniAppAvatarAdminError(w, err)
		return
	}
	current, _, err := s.miniAppAvatarAdminSession(ctx, r)
	if err == nil && current != scope {
		err = db.ErrMiniAppDenied
	}
	if err != nil {
		miniAppAvatarAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeMiniAppAvatarResolution(w http.ResponseWriter, r *http.Request, input *miniAppAvatarResolutionInput) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar resolution request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar resolution request")
		return false
	}
	return true
}

func (s *Server) miniAppAvatarAdminWrite(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppAvatarResolutionInput
	if !decodeMiniAppAvatarResolution(w, r, &input) {
		return
	}
	prepare := strings.HasSuffix(r.URL.Path, "/prepare")
	if input.OperationID == uuid.Nil || (prepare && input.Confirmed) ||
		(!prepare && (!input.Confirmed || input.ProposalID == uuid.Nil)) {
		miniAppHTTPError(w, http.StatusBadRequest, "operation and exact proposal confirmation required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppAvatarAdminSession(ctx, r)
	if err != nil {
		miniAppAvatarAdminError(w, err)
		return
	}
	var result any
	if prepare {
		result, err = s.store.PrepareMiniAppAvatarResolution(
			ctx,
			scope,
			session,
			input.OperationID,
			input.ProposalID,
			input.Arguments,
		)
	} else {
		result, err = s.store.ConfirmMiniAppAvatarResolution(
			ctx,
			scope,
			session,
			input.OperationID,
			input.ProposalID,
			input.Arguments,
		)
	}
	s.miniAppAvatarAdminResult(ctx, w, r, scope, result, err)
}

func (s *Server) miniAppAvatarAdminRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppAvatarAdminSession(ctx, r)
	if err != nil {
		miniAppAvatarAdminError(w, err)
		return
	}
	result, err := s.miniAppAvatarAdminLookup(ctx, r, scope, session)
	s.miniAppAvatarAdminResult(ctx, w, r, scope, result, err)
}

func (s *Server) miniAppAvatarAdminLookup(
	ctx context.Context,
	r *http.Request,
	scope db.AvatarDeliveryAdminScope,
	session db.MiniAppAvatarAdminSession,
) (any, error) {
	for _, key := range []string{avatarAdminDeliveryField, "proposal", "operation"} {
		raw := r.PathValue(key)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, db.ErrInvalidAvatar
		}
		if key == avatarAdminDeliveryField {
			return s.store.MiniAppAvatarAdminDelivery(ctx, scope, session, id)
		}
		selector := db.AvatarDeliveryProposalSelector{}
		if key == "proposal" {
			selector.ProposalID = id
		} else {
			selector.OperationID = id
		}
		return s.store.MiniAppAvatarResolutionProposal(ctx, scope, session, selector)
	}
	after := uuid.Nil
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = uuid.Parse(raw)
		if err != nil || after == uuid.Nil {
			return nil, db.ErrInvalidAvatar
		}
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			return nil, db.ErrInvalidAvatar
		}
	}
	return s.store.PendingMiniAppAvatarResolutions(ctx, scope, session, after, limit)
}
