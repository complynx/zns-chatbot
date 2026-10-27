package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func (s *Server) registerMiniAppMassageBindings() {
	s.registerMiniAppMassageBindingWrites()
	s.public.HandleFunc("GET /app/massage-admin/events", s.miniAppMassageBindingRead)
	s.public.HandleFunc("GET /app/massage-admin/{event}/schedule", s.miniAppMassageBindingRead)
	s.public.HandleFunc("GET /app/massage-admin/{event}/bindings", s.miniAppMassageBindingRead)
	s.public.HandleFunc("GET /app/massage-admin/{event}/bindings/{specialist}", s.miniAppMassageBindingRead)
	s.public.HandleFunc("POST /app/massage-admin/target", s.miniAppMassageBindingTarget)
}

func (s *Server) miniAppMassageAdminSession(
	ctx context.Context,
	r *http.Request,
) (db.ProfileScope, db.MiniAppMassageSession, error) {
	var session db.MiniAppMassageSession
	attempt, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return db.ProfileScope{}, session, err
	}
	principal, err := s.miniAppVerifiedPrincipal(ctx, attempt.User)
	if err != nil {
		return db.ProfileScope{}, session, err
	}
	if _, ok := principal.Roles["zinusya.admin"][s.options.OrganizationID]; !ok {
		return db.ProfileScope{}, session, db.ErrAdminDenied
	}
	session.Bearer, err = miniAppBearer(r)
	if err != nil {
		return db.ProfileScope{}, session, err
	}
	session.Generation = s.miniAppKey.Generation()
	return db.ProfileScope{
		BotID:        s.options.BotID,
		Organization: s.options.OrganizationID,
		UserID:       attempt.User.ID,
		Subject:      principal.Subject,
	}, session, nil
}

func miniAppMassageAdminError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrAdminDenied) {
		miniAppHTTPError(w, http.StatusForbidden, "current global administrator required")
		return
	}
	miniAppError(w, err)
}

func (s *Server) miniAppMassageBindingRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var event uuid.UUID
	if r.PathValue("event") != "" {
		var ok bool
		event, ok = miniAppMassageEvent(w, r)
		if !ok {
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppMassageAdminSession(ctx, r)
	if err != nil {
		miniAppMassageAdminError(w, err)
		return
	}
	result, err := s.miniAppMassageBindingReadData(ctx, r, scope, session, event)
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageBindingReadData(
	ctx context.Context,
	r *http.Request,
	scope db.ProfileScope,
	session db.MiniAppMassageSession,
	event uuid.UUID,
) (any, error) {
	if raw := r.PathValue("specialist"); raw != "" {
		return s.store.MassageBinding(ctx, scope, &session, event, raw)
	}
	if strings.HasSuffix(r.URL.Path, "/schedule") {
		return s.store.MassageBindingSchedule(ctx, scope, &session, event)
	}
	if event != uuid.Nil {
		limit := int64(agentTaskPageLimit)
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.ParseInt(raw, 10, 32)
			if err != nil || limit < 1 || limit > 20 {
				return nil, massage.ErrInvalid
			}
		}
		return s.store.MassageBindingConfiguration(
			ctx,
			scope,
			&session,
			event,
			r.URL.Query().Get("after"),
			int32(limit),
		)
	}
	after, limit, ok := miniAppOrderPage(r)
	if !ok {
		return nil, massage.ErrInvalid
	}
	return s.store.MassageBindingEvents(ctx, scope, &session, after, limit)
}

func (s *Server) miniAppMassageBindingTarget(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input db.MassageBindingTargetQuery
	if !decodeMiniAppMassage(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppMassageAdminSession(ctx, r)
	if err != nil {
		miniAppMassageAdminError(w, err)
		return
	}
	result, err := s.store.MassageBindingTargetLookup(ctx, scope, &session, input)
	s.miniAppMassageResult(ctx, w, r, result, err)
}
