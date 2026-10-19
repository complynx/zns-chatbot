package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

const settingsChangePath = "/internal/agent/administration/update_settings"
const settingsMaxCacheTTLSeconds = 300

type settingsChangePreparationResponse struct {
	Body struct {
		Status        string                     `json:"status"`
		OperationID   string                     `json:"operation_id"`
		Arguments     db.SettingsChangeArguments `json:"arguments"`
		Preconditions assignmentPreconditions    `json:"preconditions"`
		Description   string                     `json:"description"`
		ExpiresAt     time.Time                  `json:"expires_at"`
	}
}

func (s *Server) registerSettingsChanges(api huma.API) {
	registerAgentPost(
		api,
		"agent-prepare-settings-change",
		settingsChangePath+"/prepare",
		internalBodyLimit,
		s.prepareSettingsChange,
	)
	registerAgentPost(
		api,
		"agent-confirm-settings-change",
		settingsChangePath,
		internalBodyLimit,
		s.confirmSettingsChange,
	)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-settings-change-"+suffix,
			settingsChangePath+"/"+suffix,
			internalBodyLimit,
			s.settingsChangeStatus,
		)
	}
}

func settingsChangeError(err error) error {
	switch {
	case errors.Is(err, db.ErrAdminDenied):
		return huma.Error403Forbidden("active global administrator required")
	case errors.Is(err, db.ErrAdminInvalid), errors.Is(err, db.ErrSettingsChangeInvalid):
		return huma.Error400BadRequest("invalid settings change")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("settings proposal not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("settings or exact proposal changed or expired")
	default:
		return huma.Error503ServiceUnavailable("settings change uncertain or unavailable; reconcile the same operation")
	}
}

func (s *Server) prepareSettingsChange(
	ctx context.Context, in *agentReadRequest[db.SettingsChangeArguments],
) (*settingsChangePreparationResponse, error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PrepareSettingsChange(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, settingsChangeError(err)
	}
	out := &settingsChangePreparationResponse{}
	out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
	out.Body.Arguments, out.Body.Description, out.Body.ExpiresAt = prep.Arguments, prep.Description, prep.ExpiresAt
	out.Body.Preconditions.ProposalID = prep.ID
	return out, nil
}

// synchronizeSettingsTTL runs under settingsMu and reads current settings, never
// a historical receipt. Repeated polls preserve an already synchronized cache.
func (s *Server) synchronizeSettingsTTL(ctx context.Context) error {
	current, err := s.store.Settings(ctx)
	if err == nil &&
		(current.Document.AuthCacheTTLSeconds < 0 || current.Document.AuthCacheTTLSeconds > settingsMaxCacheTTLSeconds) {
		err = errors.New("invalid current IAM cache TTL")
	}
	if err != nil {
		_ = s.iam.SetPositiveCacheTTL(0)
		s.settingsTTLKnown = false
		return err
	}
	ttl := time.Duration(current.Document.AuthCacheTTLSeconds) * time.Second
	if s.settingsTTLKnown && s.settingsTTL == ttl {
		return nil
	}
	if err = s.iam.SetPositiveCacheTTL(ttl); err != nil {
		_ = s.iam.SetPositiveCacheTTL(0)
		s.settingsTTLKnown = false
		return err
	}
	s.settingsTTL, s.settingsTTLKnown = ttl, true
	return nil
}

func (s *Server) confirmSettingsChange(
	ctx context.Context, in *paymentWriteRequest[db.SettingsChangeArguments],
) (*paymentWriteResponse[db.VersionedSettings], error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
		body.Confirmation.Mode != assignmentConfirmationMode || body.Confirmation.Preconditions == nil ||
		body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
	}
	result, err := s.store.ConfirmSettingsChange(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	if errors.Is(err, db.ErrAdminDenied) || errors.Is(err, db.ErrAdminInvalid) {
		return nil, settingsChangeError(err)
	}
	// A failed commit may have succeeded remotely. Read current durable state and
	// fall back to TTL zero when it cannot be established; retain the operation ID.
	if syncErr := s.synchronizeSettingsTTL(ctx); syncErr != nil {
		return nil, huma.Error503ServiceUnavailable("current settings unavailable; reconcile the same operation")
	}
	out := &paymentWriteResponse[db.VersionedSettings]{}
	out.Body.OperationID = body.OperationID
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrSettingsChangeInvalid) || errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "settings_change_rejected",
			Message:        "Settings or exact proposal changed, is missing or expired",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, settingsChangeError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) settingsChangeStatus(
	ctx context.Context, in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.VersionedSettings], error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.SettingsChangeReceipt(ctx, scope, in.Body.OperationID)
	if errors.Is(err, db.ErrAdminDenied) || errors.Is(err, db.ErrAdminInvalid) {
		return nil, settingsChangeError(err)
	}
	if syncErr := s.synchronizeSettingsTTL(ctx); syncErr != nil {
		return nil, huma.Error503ServiceUnavailable("current settings unavailable; reconcile the same operation")
	}
	out := &paymentWriteResponse[db.VersionedSettings]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, settingsChangeError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, receipt
	return out, nil
}
