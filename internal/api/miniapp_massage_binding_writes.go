package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// Native JSON must distinguish explicit false/zero from omitted approval parameters.
type miniAppMassageBindingArguments struct {
	db.MassageBindingChange

	Active          *bool  `json:"active"`
	ExpectedVersion *int64 `json:"expected_version"`
}

func (s *Server) registerMiniAppMassageBindingWrites() {
	s.public.HandleFunc("POST /app/massage-admin/{event}/bindings/prepare", s.miniAppMassageBindingWrite)
	s.public.HandleFunc("POST /app/massage-admin/{event}/bindings", s.miniAppMassageBindingWrite)
	s.public.HandleFunc("GET /app/massage-admin/pending", s.miniAppMassageBindingStatusRead)
	s.public.HandleFunc("GET /app/massage-admin/status/operations/{operation}", s.miniAppMassageBindingStatusRead)
	s.public.HandleFunc("GET /app/massage-admin/status/proposals/{proposal}", s.miniAppMassageBindingStatusRead)
}

func (s *Server) miniAppMassageBindingWrite(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	var input miniAppMassageWriteInput[miniAppMassageBindingArguments]
	if !decodeMiniAppMassage(w, r, &input) {
		return
	}
	prepare := strings.HasSuffix(r.URL.Path, "/prepare")
	if input.OperationID == uuid.Nil || input.Arguments.EventID != event || input.Arguments.Active == nil ||
		input.Arguments.ExpectedVersion == nil ||
		(prepare && input.Confirmed) ||
		(!prepare && (!input.Confirmed || input.ProposalID.UUID == uuid.Nil)) {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"matching event, operation, explicit active/version and exact confirmation required",
		)
		return
	}
	args := input.Arguments.MassageBindingChange
	args.Active = *input.Arguments.Active
	args.ExpectedVersion = *input.Arguments.ExpectedVersion
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppMassageAdminSession(ctx, r)
	if err != nil {
		miniAppMassageAdminError(w, err)
		return
	}
	result, err := s.miniAppMassageBindingExecute(
		ctx,
		scope,
		session,
		input.OperationID,
		input.ProposalID.UUID,
		args,
		prepare,
	)
	s.miniAppMassageBindingResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageBindingExecute(
	ctx context.Context,
	scope db.ProfileScope,
	session db.MiniAppMassageSession,
	operation, proposal uuid.UUID,
	args db.MassageBindingChange,
	prepare bool,
) (any, error) {
	if !prepare {
		return s.store.ChangeMiniAppMassageBinding(
			ctx,
			scope,
			session,
			operation,
			proposal,
			args,
			func(checkCtx context.Context) error {
				// No Store access while the binding transaction holds the actor/session lock.
				principal, err := s.miniAppVerifiedPrincipal(
					checkCtx,
					db.User{
						ID:           scope.UserID,
						BotID:        scope.BotID,
						Subject:      scope.Subject,
						Organization: scope.Organization,
					},
				)
				if err != nil {
					return err
				}
				if _, admin := principal.Roles["zinusya.admin"][scope.Organization]; !admin {
					return db.ErrAdminDenied
				}
				return nil
			},
		)
	}
	var err error
	if proposal != uuid.Nil {
		_, err = s.store.AdoptMiniAppMassageBinding(ctx, scope, session, operation, proposal, args)
	} else {
		_, err = s.store.PrepareMiniAppMassageBinding(ctx, scope, session, operation, args)
	}
	if err != nil {
		return nil, err
	}
	return s.store.MassageBindingStatus(ctx, scope, &session, operation, uuid.Nil)
}

func (s *Server) miniAppMassageBindingResult(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	result any,
	err error,
) {
	if errors.Is(err, db.ErrAdminDenied) || errors.Is(err, identity.ErrDenied) {
		miniAppMassageAdminError(w, err)
		return
	}
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageBindingStatusRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppMassageAdminSession(ctx, r)
	if err != nil {
		miniAppMassageAdminError(w, err)
		return
	}
	var result any
	switch {
	case r.PathValue("operation") != "":
		var id uuid.UUID
		id, err = requiredMassageBindingUUID(r.PathValue("operation"))
		if err == nil {
			result, err = s.store.MassageBindingStatus(ctx, scope, &session, id, uuid.Nil)
		}
	case r.PathValue("proposal") != "":
		var id uuid.UUID
		id, err = requiredMassageBindingUUID(r.PathValue("proposal"))
		if err == nil {
			result, err = s.store.MassageBindingStatus(ctx, scope, &session, uuid.Nil, id)
		}
	default:
		var event, after uuid.UUID
		event, err = optionalMassageBindingUUID(r.URL.Query().Get("event_id"))
		if err == nil {
			after, err = optionalMassageBindingUUID(r.URL.Query().Get("after"))
		}
		if err == nil {
			result, err = s.store.PendingMassageBindings(ctx, scope, &session, event, after)
		}
	}
	s.miniAppMassageBindingResult(ctx, w, r, result, err)
}

func optionalMassageBindingUUID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	return requiredMassageBindingUUID(raw)
}

func requiredMassageBindingUUID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, massage.ErrInvalid
	}
	return id, nil
}
