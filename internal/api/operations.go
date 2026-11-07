package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type accountRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                        `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string                     `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string                     `json:"run_id,omitempty"`
		Arguments       map[string]json.RawMessage `json:"arguments"`
	}
}
type accountResult struct {
	ID             uuid.UUID       `json:"id"`
	TelegramUserID int64           `json:"telegram_user_id"`
	Profile        json.RawMessage `json:"profile"`
}
type accountResponse struct {
	Body struct {
		Status      string        `json:"status"`
		OperationID string        `json:"operation_id"`
		Result      accountResult `json:"result"`
	}
}
type settingsResponse struct{ Body db.VersionedSettings }
type settingsRequest struct {
	AuthInput

	Body db.VersionedSettings
}
type taskRequest struct {
	AuthInput

	ID uuid.UUID `path:"id"`
}
type taskResponse struct {
	Body struct {
		ID    uuid.UUID `json:"id"`
		State string    `json:"state"`
		RunID string    `json:"run_id,omitempty"`
	}
}
type revokeRequest struct {
	AuthInput

	ID uuid.UUID `path:"id"`
}
type revokeResponse struct {
	Body struct {
		ID      uuid.UUID `json:"id"`
		Revoked bool      `json:"revoked"`
	}
}

func (s *Server) register(api huma.API) {
	s.registerKnowledge(api)
	s.registerAgentAdministration(api)
	s.registerArchiveValidation(api)
	s.registerArchiveStorage(api)
	s.registerHistory(api)
	s.registerEvents(api)
	s.registerOrdersCatalog(api)
	s.registerMealCatalog(api)
	s.registerMassageSchedule(api)
	s.registerPartnerCode(api)
	s.registerAuthorities(api)
	s.registerPayments(api)
	s.registerOrderEvidence(api)
	s.registerNotificationAPI(api)
	s.registerNotificationResolutions(api)
	s.registerAvatarDeliveryResolutions(api)
	s.registerRegistrations(api)
	s.registerAgentRegistrations(api)
	s.registerPairReads(api)
	s.registerPairAssignments(api)
	s.registerAgentAssignments(api)
	s.registerAgentEventContext(api)
	s.registerProfile(api)
	s.registerRegistrationDetails(api)
	s.registerAvatars(api)
	s.registerTaskAdministration(api)
	s.registerTaskClosures(api)
	s.registerSettingsChanges(api)
	s.registerUserRevocations(api)
	s.registerEventChanges(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "account-me",
			Method:       http.MethodPost,
			Path:         "/internal/agent/account/me",
			MaxBodyBytes: internalBodyLimit,
		},
		s.accountMe,
	)
	huma.Register(
		api,
		huma.Operation{OperationID: "get-settings", Method: http.MethodGet, Path: "/internal/admin/settings"},
		s.getSettings,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "update-settings",
			Method:       http.MethodPut,
			Path:         "/internal/admin/settings",
			MaxBodyBytes: internalBodyLimit,
		},
		s.updateSettings,
	)
	huma.Register(
		api,
		huma.Operation{OperationID: "get-task", Method: http.MethodGet, Path: "/internal/tasks/{id}"},
		s.getTask,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "revoke-user",
			Method:       http.MethodPost,
			Path:         "/internal/admin/users/{id}/revoke",
			MaxBodyBytes: internalBodyLimit,
		},
		s.revokeUser,
	)
}

func (s *Server) accountMe(ctx context.Context, in *accountRequest) (*accountResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Arguments) != 0 {
		return nil, huma.Error400BadRequest("account.me takes no arguments")
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &accountResponse{}
	out.Body.Status = agentSucceeded
	out.Body.OperationID = in.Body.OperationID
	out.Body.Result = accountResult{u.ID, u.TelegramUserID, u.Profile}
	return out, nil
}

func (s *Server) getSettings(ctx context.Context, in *AuthInput) (*settingsResponse, error) {
	_, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.Settings(ctx)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("settings unavailable")
	}
	return &settingsResponse{Body: result}, nil
}

func (s *Server) updateSettings(ctx context.Context, in *settingsRequest) (*settingsResponse, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	d := in.Body.Document
	_, miniAppPolicyErr := d.MiniAppPolicy()
	if miniAppPolicyErr != nil || in.Body.Version < 1 || d.AuthCacheTTLSeconds < 0 || d.AuthCacheTTLSeconds > 300 ||
		(d.DefaultLanguage != "en" && d.DefaultLanguage != "ru") ||
		d.MaxParallelTasks < 1 ||
		d.MaxParallelTasks > 128 ||
		d.QuestionTimeoutSeconds < 1 ||
		d.QuestionTimeoutSeconds > 604800 ||
		(d.MassageMaxBookingsPerWindow < 0 || d.MassageMaxBookingsPerWindow > 100) ||
		(d.NameExpectationSeconds != 0 && (d.NameExpectationSeconds < 60 || d.NameExpectationSeconds > 86400)) {
		return nil, huma.Error400BadRequest("invalid settings")
	}
	result, err := s.store.CompareAndSwapSettings(ctx, in.Body.Version, d, p.Subject)
	if errors.Is(err, db.ErrConflict) {
		return nil, huma.Error409Conflict("settings version changed")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("settings unavailable")
	}
	if err = s.iam.SetPositiveCacheTTL(time.Duration(d.AuthCacheTTLSeconds) * time.Second); err != nil {
		return nil, huma.Error503ServiceUnavailable("cache configuration unavailable")
	}
	s.settingsTTL, s.settingsTTLKnown = time.Duration(d.AuthCacheTTLSeconds)*time.Second, true
	return &settingsResponse{Body: result}, nil
}

func (s *Server) getTask(ctx context.Context, in *taskRequest) (*taskResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return nil, err
	}
	t, err := s.store.GetTask(ctx, in.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && t.UserID != u.ID) {
		return nil, huma.Error404NotFound("task not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("task unavailable")
	}
	out := &taskResponse{}
	out.Body.ID = t.ID
	out.Body.State = t.State
	out.Body.RunID = t.RunID
	return out, nil
}

func (s *Server) revokeUser(ctx context.Context, in *revokeRequest) (*revokeResponse, error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	u, err := s.store.RevokeScopedUser(ctx, scope, in.ID)
	if u.Subject != "" {
		s.iam.Invalidate(u.Subject)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("user not found")
	}
	if errors.Is(err, db.ErrAdminDenied) {
		return nil, huma.Error403Forbidden("active global administrator required")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("revocation unavailable")
	}
	out := &revokeResponse{}
	out.Body.ID = u.ID
	out.Body.Revoked = u.Revoked
	return out, nil
}
