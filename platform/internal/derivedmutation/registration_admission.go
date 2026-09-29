package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// CapturePassAdmission fences derived evidence in the same transaction as the
// durable intent, before profile completion or a subsequent booking transaction.
func (s Service) CapturePassAdmission(
	ctx context.Context,
	actor string,
	request passbooking.AdmissionRequest,
	source readsource.Derivation,
) (passbooking.Admission, error) {
	if !passbooking.InitiatesRegistration(request.Command) {
		return passbooking.Admission{}, nil
	}
	if !source.Valid() {
		return passbooking.Admission{}, invalidSource()
	}
	tx, err := s.beginSourceMutation(ctx, []string{actor, request.Command.Target}, source)
	if err != nil {
		return passbooking.Admission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Preserve target authorization and stale-version denial before source capture.
	prepared, err := s.Registration.PrepareAdmissionInTx(ctx, tx, actor, request.Command)
	if err != nil {
		return passbooking.Admission{}, err
	}
	if prepared.Replayed() && request.Ingress == nil {
		return passbooking.Admission{}, nil
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return passbooking.Admission{}, err
	}
	result, err := prepared.Capture(ctx, request.Ingress)
	if err != nil {
		return passbooking.Admission{}, err
	}
	return result, tx.Commit(ctx)
}
