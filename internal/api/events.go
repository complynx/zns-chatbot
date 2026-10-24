package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

type eventRequest struct {
	AuthInput

	ID uuid.UUID `path:"id"`
}
type eventResponse struct{ Body events.Event }
type eventListRequest struct {
	AuthInput

	After uuid.UUID `query:"after"`
	Limit int32     `query:"limit" default:"50" minimum:"1" maximum:"100"`
}
type eventListResponse struct {
	Body struct {
		Events []events.Event `json:"events"`
		Next   *uuid.UUID     `json:"next,omitempty"`
	}
}
type eventWriteRequest struct {
	AuthInput

	ID   uuid.UUID `path:"id"`
	Body struct {
		OperationID string `json:"operation_id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$"`

		ExpectedVersion int64           `json:"expected_version" minimum:"0"`
		Document        events.Document `json:"document"`
	}
}
type eventOperationRequest struct {
	AuthInput

	OperationID string `path:"operation_id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$"`
}

func (s *Server) registerEvents(api huma.API) {
	huma.Register(
		api,
		huma.Operation{OperationID: "admin-list-events", Method: http.MethodGet, Path: "/internal/admin/events"},
		s.listEvents,
	)
	huma.Register(
		api,
		huma.Operation{OperationID: "admin-read-event", Method: http.MethodGet, Path: "/internal/admin/events/{id}"},
		s.readEvent,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "admin-write-event",
			Method:       http.MethodPut,
			Path:         "/internal/admin/events/{id}",
			MaxBodyBytes: internalBodyLimit,
		},
		s.writeEvent,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID: "admin-event-operation",
			Method:      http.MethodGet,
			Path:        "/internal/admin/event-operations/{operation_id}",
		},
		s.eventOperation,
	)
}
func (s *Server) eventScope() db.EventScope {
	return db.EventScope{BotID: s.options.BotID, Organization: s.options.OrganizationID}
}

func (s *Server) listEvents(ctx context.Context, in *eventListRequest) (*eventListResponse, error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	result, err := s.store.Events(ctx, s.eventScope(), in.After, in.Limit)
	if err != nil {
		return nil, eventError(err)
	}
	out := &eventListResponse{}
	out.Body.Events = result
	if len(result) == int(in.Limit) {
		out.Body.Next = &result[len(result)-1].ID
	}
	return out, nil
}
func (s *Server) readEvent(ctx context.Context, in *eventRequest) (*eventResponse, error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	result, err := s.store.Event(ctx, s.eventScope(), in.ID)
	if err != nil {
		return nil, eventError(err)
	}
	return &eventResponse{Body: result}, nil
}
func (s *Server) writeEvent(ctx context.Context, in *eventWriteRequest) (*eventResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ChangeEvent(
		ctx,
		s.eventScope(),
		p.Subject,
		in.Body.OperationID,
		db.EventChange{ID: in.ID, ExpectedVersion: in.Body.ExpectedVersion, Document: in.Body.Document},
	)
	if err != nil {
		return nil, eventError(err)
	}
	return &eventResponse{Body: result}, nil
}
func (s *Server) eventOperation(ctx context.Context, in *eventOperationRequest) (*eventResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.EventOperation(ctx, s.eventScope(), p.Subject, in.OperationID)
	if err != nil {
		return nil, eventError(err)
	}
	return &eventResponse{Body: result}, nil
}
func eventError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return huma.Error404NotFound("event or operation not found")
	}
	if errors.Is(err, db.ErrConflict) {
		return huma.Error409Conflict("event version or operation changed")
	}
	if errors.Is(err, events.ErrInvalid) {
		return huma.Error400BadRequest("invalid event configuration")
	}
	return huma.Error503ServiceUnavailable("event storage unavailable")
}
