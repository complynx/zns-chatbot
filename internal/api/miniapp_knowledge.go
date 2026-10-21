package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Native knowledge routes share canonical article, suggestion and operation IDs
// with agent tools. Every request uses the current server-resolved actor.
func (s *Server) registerMiniAppKnowledge() {
	s.registerMiniAppKnowledgeActions()
	s.registerMiniAppKnowledgeReads()
	registerMiniAppKnowledgeWrite(s, "suggest", knowledgeEditChange, knowledgeEditFromChange)
	registerMiniAppKnowledgeWrite(s, "edit", knowledgeEditChange, knowledgeEditFromChange)
	registerMiniAppKnowledgeWrite(s, "review", knowledgeReviewChange, knowledgeReviewFromChange)
	registerMiniAppKnowledgeWrite(
		s,
		"set_moderator",
		miniAppKnowledgeModeratorChange,
		miniAppKnowledgeModeratorFromChange,
	)
}

func (s *Server) miniAppKnowledgeSession(
	ctx context.Context,
	r *http.Request,
) (db.PaymentScope, db.MiniAppKnowledgeSession, error) {
	scope := db.PaymentScope{EventScope: s.eventScope()}
	var session db.MiniAppKnowledgeSession
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
	session.Contract = s.knowledgeContract()
	session.UserID = attempt.User.ID
	session.Generation = s.miniAppKey.Generation()
	session.AuthorityUnchanged = func() bool { return unchanged() && time.Now().Before(principal.ExpiresAt) }
	session.Bearer, err = miniAppBearer(r)
	if err == nil && !session.AuthorityUnchanged() {
		err = db.ErrMiniAppDenied
	}
	if err == nil {
		current, e := s.currentMiniApp(ctx, r)
		err = e
		if err == nil && (current.User.ID != scope.UserID || current.User.Subject != scope.Subject) {
			err = db.ErrMiniAppDenied
		}
	}
	return scope, session, err
}

// Decode a single non-null object. Unknown fields are rejected recursively by
// encoding/json, including the per-operation argument struct.
func decodeMiniAppKnowledge[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var value *T
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, knowledgeTransportLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return *new(T), err
	}
	if value == nil {
		return *new(T), errors.New("JSON object required")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return *new(T), errors.New("one JSON object required")
	}
	return *value, nil
}

func miniAppKnowledgeError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrMiniAppDenied) {
		miniAppError(w, err)
		return
	}
	mapped := knowledgeError(err)
	if status, ok := errors.AsType[huma.StatusError](mapped); ok {
		miniAppHTTPError(w, status.GetStatus(), status.Error())
		return
	}
	miniAppError(w, err)
}

func registerMiniAppKnowledgePost[T any](
	s *Server,
	path string,
	run func(context.Context, db.PaymentScope, db.MiniAppKnowledgeSession, T) (any, error),
) {
	s.public.HandleFunc("POST /app/knowledge/"+path, func(w http.ResponseWriter, r *http.Request) {
		if !s.miniAppRequest(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
		defer cancel()
		scope, session, err := s.miniAppKnowledgeSession(ctx, r)
		if err != nil {
			miniAppError(w, err)
			return
		}
		args, err := decodeMiniAppKnowledge[T](w, r)
		if err != nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid knowledge request")
			return
		}
		result, err := run(ctx, scope, session, args)
		if err != nil {
			miniAppKnowledgeError(w, err)
			return
		}
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > knowledgeResultLimit {
			miniAppHTTPError(w, http.StatusServiceUnavailable, "knowledge response exceeds transport limit")
			return
		}
		current, fresh, err := s.miniAppKnowledgeSession(ctx, r)
		if err == nil && (current != scope || fresh.UserID != session.UserID || fresh.Bearer != session.Bearer ||
			fresh.Generation != session.Generation || !session.AuthorityUnchanged()) {
			err = db.ErrMiniAppDenied
		}
		if err != nil {
			miniAppError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(encoded)
	})
}
