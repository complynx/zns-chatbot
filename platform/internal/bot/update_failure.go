package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type updateFailurePhase uint8

const (
	updateFailureIngress updateFailurePhase = iota + 1
	updateFailureHandler
)

const (
	updateFailureUnknown      = "unknown"
	updateFailureDomainDenied = "domain_denied"
	maxUpdateProviderCode     = 599
)

type updateFailure struct {
	code         string
	providerCode int
}

// observeUpdateFailure reports provenance only. Inbox scheduling and durable
// delivery outcomes remain owned elsewhere; an error cannot prove uncertainty.
func (b *Bot) observeUpdateFailure(ctx context.Context, phase updateFailurePhase, err error) {
	if err == nil {
		return
	}
	name := updateFailureUnknown
	switch phase {
	case updateFailureIngress:
		name = "ingress"
	case updateFailureHandler:
		name = "handler"
	}
	failure := classifyUpdateFailure(err)
	b.logger().WarnContext(ctx, "telegram update failed",
		"operation", "telegram.update", "phase", name, "code", failure.code,
		"retryability", updateFailureUnknown, "transport_status", 0, "provider_code", failure.providerCode)
}

// classifyUpdateFailure reuses existing typed provenance, never error text.
// Positive SQL origin wins over joined cancellation and domain rejection.
func classifyUpdateFailure(err error) updateFailure {
	if core.IsDatabaseFailure(err) {
		if errors.Is(err, core.ErrDatabaseSerialization) {
			return updateFailure{code: "database_serialization"}
		}
		return updateFailure{code: "database_failure"}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return updateFailure{code: "canceled"}
	case errors.Is(err, context.DeadlineExceeded):
		return updateFailure{code: "timeout"}
	case errors.Is(err, errHistoryPlanTerminal), errors.Is(err, errPassPlanTerminal):
		return updateFailure{code: "terminal_plan"}
	case errors.Is(err, appclient.ErrReadStale):
		return updateFailure{code: "stale_reference"}
	case errors.Is(err, identity.ErrZitadelIdentity),
		errors.Is(err, identity.ErrZitadelUserInactive),
		errors.Is(err, appclient.ErrProvisioningDenied):
		return updateFailure{code: updateFailureDomainDenied}
	}
	if _, ok := errors.AsType[creditCutoverError](err); ok {
		return updateFailure{code: "credit_configuration"}
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem != nil {
		switch problem.Code {
		case "stale_version", "history_stale":
			return updateFailure{code: "stale_reference"}
		case "source_revoked":
			return updateFailure{code: updateFailureDomainDenied}
		}
		if problem.Status == http.StatusUnauthorized || problem.Status == http.StatusForbidden {
			return updateFailure{code: updateFailureDomainDenied}
		}
		if problem.Status >= http.StatusBadRequest && problem.Status < http.StatusInternalServerError {
			return updateFailure{code: "domain_rejected"}
		}
	}
	return classifyUpdateProviderFailure(err)
}

func classifyUpdateProviderFailure(err error) updateFailure {
	provider := 0
	if api, ok := errors.AsType[*telegram.APIError](
		err,
	); ok && api != nil && api.Code >= http.StatusContinue &&
		api.Code <= maxUpdateProviderCode {
		provider = api.Code
	}
	if control, ok := errors.AsType[*telegram.ControlError](err); ok && control != nil {
		return updateFailure{code: "provider_deferred", providerCode: provider}
	}
	if provider != 0 {
		code := "provider_failure"
		switch provider {
		case http.StatusUnauthorized, http.StatusForbidden:
			code = "provider_denied"
		case http.StatusTooManyRequests:
			code = "provider_rate_limited"
		}
		return updateFailure{code: code, providerCode: provider}
	}
	return updateFailure{code: "operation_failed"}
}
