package derivedmutation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative/dbgen"
)

// NativeRegistrationAuthorizer refreshes the original sender identity, not the
// identity of the HTTP caller that happened to drain the event. False is a
// definitive denial; an error preserves the pending event barrier.
type NativeRegistrationAuthorizer func(context.Context, string, int64, int64) (bool, error)

type NativeRegistrationResolver struct {
	Service   Service
	Authorize NativeRegistrationAuthorizer
}

func (r NativeRegistrationResolver) ResolveRegistrationIntake(ctx context.Context, event string) error {
	rows, err := dbgen.New(r.Service.DB).PendingNativeCandidates(ctx, event)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if len(rows) == 0 {
		return nil
	}
	if r.Authorize == nil {
		return errors.New("native registration identity resolver is not configured")
	}
	for _, row := range rows {
		var envelope registrationnative.Envelope
		if err = json.Unmarshal(row.NativePayload, &envelope); err != nil {
			return fmt.Errorf("decode native registration evidence: %w", err)
		}
		if envelope.Command.Event != event || envelope.Chat != row.TelegramID {
			return errors.New("native registration evidence binding mismatch")
		}
		allowed, authErr := r.Authorize(ctx, envelope.Owner, row.BotID, row.TelegramID)
		if authErr != nil {
			return authErr
		}
		if err = r.resolveCandidate(ctx, row, envelope, allowed); err != nil {
			return err
		}
	}
	return nil
}

func (r NativeRegistrationResolver) resolveCandidate(
	ctx context.Context,
	row dbgen.PendingNativeCandidatesRow,
	envelope registrationnative.Envelope,
	allowed bool,
) error {
	tx, err := r.Service.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	registration, clockAttempt := r.Service.Registration.WithClockAttempt()
	r.Service.Registration = registration
	var source readsource.Derivation
	if envelope.Source != nil {
		source = envelope.Source.Clone()
	}
	if err = lockRegistrationPrelude(
		ctx,
		tx,
		envelope.Owner,
		envelope.Command.Target,
		envelope.Command.Event,
		source,
	); err != nil {
		return err
	}
	outcome, intentID := "rejected", int64(0)
	if allowed {
		admission, captureErr := r.captureCandidateAttempt(ctx, tx, row, envelope)
		if captureErr == nil {
			outcome, intentID = "admitted", admission.ID
		}
		if captureErr != nil && !nativeRegistrationRefusal(captureErr) {
			return captureErr
		}
	}
	if err = dbgen.New(tx).CompleteNativeCandidate(ctx, dbgen.CompleteNativeCandidateParams{
		ID:       row.ID,
		Payload:  row.NativePayload,
		Outcome:  outcome,
		IntentID: pgtype.Int8{Int64: intentID, Valid: intentID > 0},
	}); err != nil {
		return core.DatabaseOperationError(err)
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (r NativeRegistrationResolver) captureCandidate(
	ctx context.Context,
	tx pgx.Tx,
	row dbgen.PendingNativeCandidatesRow,
	envelope registrationnative.Envelope,
) (passbooking.Admission, error) {
	prepared, err := r.Service.Registration.PrepareAdmissionInTx(ctx, tx, envelope.Owner, envelope.Command)
	if err != nil {
		return passbooking.Admission{}, err
	}
	if envelope.Source != nil {
		if err = lockSource(ctx, tx, envelope.Owner, *envelope.Source); err != nil {
			return passbooking.Admission{}, err
		}
	}
	valid, err := registrationnative.Check(ctx, tx, envelope)
	if err != nil {
		return passbooking.Admission{}, err
	}
	if !valid {
		return passbooking.Admission{}, &core.ProblemError{
			Status: http.StatusConflict,
			Code:   "native_registration_stale",
		}
	}
	updateID, err := strconv.ParseInt(row.RequestKey, 10, 64)
	if err != nil {
		return passbooking.Admission{}, err
	}
	result, err := prepared.CaptureNative(ctx, &registrationingress.Reference{BotID: row.BotID, UpdateID: updateID})
	if err != nil {
		return result, err
	}
	err = r.Service.Registration.RefreshAdmissionTurnsInTx(ctx, tx, envelope.Command.Event)
	return result, err
}

func nativeRegistrationRefusal(err error) bool {
	if core.IsDatabaseFailure(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var problem *core.ProblemError
	if !errors.As(err, &problem) {
		return false
	}
	return problem.Status == http.StatusBadRequest || problem.Status == http.StatusForbidden ||
		problem.Status == http.StatusNotFound ||
		problem.Status == http.StatusConflict
}

// A savepoint discards only this candidate's partial capture on refusal.
func (r NativeRegistrationResolver) captureCandidateAttempt(
	ctx context.Context,
	tx pgx.Tx,
	row dbgen.PendingNativeCandidatesRow,
	envelope registrationnative.Envelope,
) (passbooking.Admission, error) {
	attempt, err := tx.Begin(ctx)
	if err != nil {
		return passbooking.Admission{}, core.DatabaseOperationError(err)
	}
	admission, err := r.captureCandidate(ctx, attempt, row, envelope)
	if err == nil {
		return admission, core.DatabaseOperationError(attempt.Commit(ctx))
	}
	if rollbackErr := attempt.Rollback(ctx); rollbackErr != nil {
		rollbackFailure := core.DatabaseOperationError(rollbackErr)
		if core.IsDatabaseFailure(rollbackFailure) {
			return passbooking.Admission{}, rollbackFailure
		}
		return passbooking.Admission{}, errors.Join(err, rollbackFailure)
	}
	return passbooking.Admission{}, err
}
