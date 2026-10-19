package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type registrationResponse struct{ Body registrations.Registration }
type registrationWriteRequest struct {
	AuthInput

	ID   uuid.UUID `path:"id"`
	Body struct {
		OperationID string `json:"operation_id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$"`
		Role        string `json:"role" enum:"leader,follower"`
	}
}
type assignmentRequest struct {
	AuthInput

	ID             uuid.UUID `path:"id"`
	RegistrationID uuid.UUID `path:"registration_id"`
	Body           struct {
		OperationID          string `json:"operation_id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$"`
		ExpectedVersion      int64  `json:"expected_version" minimum:"1"`
		ExpectedEventVersion int64  `json:"expected_event_version" minimum:"1"`
	}
}
type registrationListRequest struct {
	AuthInput

	ID    uuid.UUID `path:"id"`
	After uuid.UUID `          query:"after"`
	Limit int32     `          query:"limit" default:"50" minimum:"1" maximum:"100"`
}
type registrationListResponse struct {
	Body struct {
		Registrations []registrations.Registration `json:"registrations"`
		Next          *uuid.UUID                   `json:"next,omitempty"`
	}
}

func (s *Server) registerRegistrations(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID: "registration-read",
			Method:      http.MethodGet,
			Path:        "/internal/events/{id}/registration",
		},
		s.readRegistration,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "registration-request",
			Method:       http.MethodPost,
			Path:         "/internal/events/{id}/registration",
			MaxBodyBytes: internalBodyLimit,
		},
		s.requestRegistration,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID: "admin-registration-list",
			Method:      http.MethodGet,
			Path:        "/internal/admin/events/{id}/registrations",
		},
		s.listRegistrations,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "admin-pass-assign",
			Method:       http.MethodPost,
			Path:         "/internal/admin/events/{id}/registrations/{registration_id}/assign",
			MaxBodyBytes: internalBodyLimit,
		},
		s.assignRegistration,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID: "registration-operation",
			Method:      http.MethodGet,
			Path:        "/internal/registration-operations/{operation_id}",
		},
		s.registrationOperation,
	)
}
func (s *Server) readRegistration(ctx context.Context, in *eventRequest) (*registrationResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	user, err := s.account(ctx, p)
	if err != nil {
		return nil, err
	}
	result, err := s.store.Registration(ctx, s.eventScope(), in.ID, user.ID)
	if err != nil {
		return nil, registrationError(err)
	}
	return &registrationResponse{Body: result}, nil
}
func (s *Server) requestRegistration(ctx context.Context, in *registrationWriteRequest) (*registrationResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	user, err := s.account(ctx, p)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ChangeRegistration(
		ctx,
		s.eventScope(),
		p.Subject,
		in.Body.OperationID,
		db.RegistrationChange{Kind: "register", EventID: in.ID, UserID: user.ID, Role: in.Body.Role},
	)
	if err != nil {
		return nil, registrationError(err)
	}
	return &registrationResponse{Body: result}, nil
}
func (s *Server) assignRegistration(ctx context.Context, in *assignmentRequest) (*registrationResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ChangeRegistration(
		ctx,
		s.eventScope(),
		p.Subject,
		in.Body.OperationID,
		db.RegistrationChange{
			Kind:                 "assign",
			EventID:              in.ID,
			RegistrationID:       in.RegistrationID,
			ExpectedVersion:      in.Body.ExpectedVersion,
			ExpectedEventVersion: in.Body.ExpectedEventVersion,
		},
	)
	if err != nil {
		return nil, registrationError(err)
	}
	return &registrationResponse{Body: result}, nil
}

func (s *Server) listRegistrations(
	ctx context.Context,
	in *registrationListRequest,
) (*registrationListResponse, error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	result, err := s.store.EventRegistrations(ctx, s.eventScope(), in.ID, in.After, in.Limit)
	if err != nil {
		return nil, registrationError(err)
	}
	out := &registrationListResponse{}
	out.Body.Registrations = result
	if len(result) == int(in.Limit) {
		out.Body.Next = &result[len(result)-1].ID
	}
	return out, nil
}
func (s *Server) registrationOperation(ctx context.Context, in *eventOperationRequest) (*registrationResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	result, err := s.store.RegistrationOperation(ctx, s.eventScope(), p.Subject, in.OperationID)
	if err != nil {
		return nil, registrationError(err)
	}
	return &registrationResponse{Body: result}, nil
}
func registrationError(err error) error {
	switch {
	case errors.Is(err, registrationdetails.ErrRequired):
		return huma.Error409Conflict("required legal registration details missing")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("registration, account, event or operation not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("registration version, event version or operation changed")
	case errors.Is(err, registrations.ErrCapacity):
		return huma.Error409Conflict("event role capacity reached")
	case errors.Is(err, registrations.ErrUnavailable):
		return huma.Error409Conflict("registration unavailable or sales closed")
	case errors.Is(err, registrations.ErrInvalid):
		return huma.Error400BadRequest("invalid registration request")
	default:
		return huma.Error503ServiceUnavailable("registration storage unavailable")
	}
}
