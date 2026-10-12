package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/shuttle/internal/db"
)

type adminTaskListRequest struct {
	AuthInput

	State string    `query:"state" maxLength:"64"`
	After uuid.UUID `query:"after"`
	Limit int32     `query:"limit"                default:"50" minimum:"1" maximum:"100"`
}

type adminTaskListResponse struct {
	Body struct {
		Tasks []db.TaskSummary `json:"tasks"`
		Next  *uuid.UUID       `json:"next,omitempty"`
	}
}

type adminTaskResponse struct{ Body db.TaskDiagnostics }

type closeTaskRequest struct {
	AuthInput

	ID   uuid.UUID `path:"id"`
	Body struct {
		OperationID     string `json:"operation_id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$"`
		ExpectedVersion int64  `json:"expected_version" minimum:"1"`
		Reason          string `json:"reason" minLength:"1" maxLength:"512"`
		Confirm         bool   `json:"confirm"`
	}
}

type closeTaskResponse struct{ Body db.TaskResolution }

func (s *Server) registerTaskAdministration(api huma.API) {
	huma.Register(
		api,
		huma.Operation{OperationID: "admin-list-tasks", Method: http.MethodGet, Path: "/internal/admin/tasks"},
		s.adminListTasks,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID: "admin-task-diagnostics",
			Method:      http.MethodGet,
			Path:        "/internal/admin/tasks/{id}",
		},
		s.adminTask,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "admin-close-uncertain-task",
			Method:       http.MethodPost,
			Path:         "/internal/admin/tasks/{id}/close",
			MaxBodyBytes: internalBodyLimit,
		},
		s.closeTask,
	)
}

func (s *Server) adminListTasks(ctx context.Context, in *adminTaskListRequest) (*adminTaskListResponse, error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	tasks, err := s.store.ListAdminTasks(ctx, s.options.BotID, s.options.OrganizationID, in.State, in.After, in.Limit)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("task diagnostics unavailable")
	}
	out := &adminTaskListResponse{}
	out.Body.Tasks = tasks
	if len(tasks) == int(in.Limit) {
		out.Body.Next = &tasks[len(tasks)-1].ID
	}
	return out, nil
}

func (s *Server) adminTask(ctx context.Context, in *taskRequest) (*adminTaskResponse, error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	result, err := s.store.TaskDiagnostics(ctx, in.ID, s.options.BotID, s.options.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("task not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("task diagnostics unavailable")
	}
	return &adminTaskResponse{Body: result}, nil
}

func (s *Server) closeTask(ctx context.Context, in *closeTaskRequest) (*closeTaskResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	if !in.Body.Confirm || strings.TrimSpace(in.Body.Reason) == "" {
		return nil, huma.Error400BadRequest("explicit confirmation and reason required")
	}
	result, err := s.store.CloseUncertainTask(ctx, db.CloseTaskRequest{
		TaskID:          in.ID,
		BotID:           s.options.BotID,
		Organization:    s.options.OrganizationID,
		OperationID:     in.Body.OperationID,
		Actor:           p.Subject,
		Reason:          in.Body.Reason,
		ExpectedVersion: in.Body.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("task not found")
	}
	if errors.Is(err, db.ErrConflict) {
		return nil, huma.Error409Conflict("task changed, is leased, or has no uncertain outcome")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("task closure unavailable")
	}
	return &closeTaskResponse{Body: result}, nil
}
