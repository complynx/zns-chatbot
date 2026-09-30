package passbooking

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

const admissionRejected = "rejected"

// Native evidence supplies reception time, not a historical sales-open claim.
// Zero time means that the request had no native classification.
func (p *PreparedCommand) nativeAdmissionTime(
	ctx context.Context,
	ref *registrationingress.Reference,
) (time.Time, error) {
	if ref == nil {
		return time.Time{}, nil
	}
	position, err := p.admissionPosition(ctx, ref)
	if err != nil {
		return time.Time{}, err
	}
	evidence, err := dbgen.New(p.tx).NativeAdmissionEvidence(ctx, dbgen.NativeAdmissionEvidenceParams{
		ID: position, Event: p.command.Event, Owner: p.actor, Key: p.command.Key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, core.DatabaseOperationError(err)
	}
	if evidence.NativeOutcome == admissionRejected {
		return time.Time{}, conflict("pass_admission_rejected")
	}
	return evidence.ReceivedAt.Time, nil
}
