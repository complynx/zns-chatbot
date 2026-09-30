package interaction_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type observationFailurePorts struct {
	failure error
}

func (p observationFailurePorts) ReadRegistrationOperations(
	context.Context, string, string,
) ([]interaction.RegistrationOperation, error) {
	return []interaction.RegistrationOperation{{
		ID: "ABCDEFGHIJKLMNOPQRSTUVWXYZ", Tool: "passes.registration.invite", Retired: true,
		Command: &passbooking.Command{Name: "invite", Event: "dance", Key: "key"}}}, nil
}

func (p observationFailurePorts) ReadPassOperation(
	context.Context, string, derivedmutation.PassOperationInput,
) (derivedmutation.PassOperationRead, error) {
	return derivedmutation.PassOperationRead{}, p.failure
}

func TestReceiptObservationSQLAndCancellationProvenance(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{core.DatabaseOperationError(io.ErrUnexpectedEOF), context.Canceled} {
		ports := observationFailurePorts{failure: failure}
		operations := interaction.RegistrationOperations{Ledger: ports, Domain: ports}
		value, err := operations.ObserveReceipt(t.Context(), "alice", "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		require.ErrorIs(t, err, failure)
		require.Equal(t, interaction.RegistrationReceiptObservation{}, value)
		require.Equal(t, !errors.Is(failure, context.Canceled), core.IsDatabaseFailure(err))
	}
}
