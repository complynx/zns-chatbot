package interaction_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type operationReadPorts struct {
	calls []string
	fail  func(context.Context) error
}

func (*operationReadPorts) ReadRegistrationOperations(
	_ context.Context, _, _ string,
) ([]interaction.RegistrationOperation, error) {
	var admissions []interaction.RegistrationOperation
	for _, id := range []string{"first", "second", "third"} {
		admissions = append(admissions, interaction.RegistrationOperation{
			ID: id, Tool: "passes.invite",
			Command: &passbooking.Command{Name: "invite", Event: "dance", Key: id},
		})
	}
	return admissions, nil
}

func (p *operationReadPorts) ReadPassOperation(
	ctx context.Context, owner string, input derivedmutation.PassOperationInput,
) (derivedmutation.PassOperationRead, error) {
	p.calls = append(p.calls, owner+"/"+input.Command.Key)
	if input.Command.Key == "second" {
		return derivedmutation.PassOperationRead{}, p.fail(ctx)
	}
	return derivedmutation.PassOperationRead{
		Summary: derivedmutation.PassOperationSummary{Status: "committed", Continuation: "unavailable"},
		ReadAuthorities: readsource.Registration([]passbooking.ReadAuthority{
			{Kind: passbooking.ReadCapability, Event: "dance", Action: "invite"},
		}),
	}, nil
}

func TestRegistrationOperationsReadDatabaseFailureOverridesDenial(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			for _, sqlFirst := range []bool{false, true} {
				t.Run(map[bool]string{false: "domain-first", true: "sql-first"}[sqlFirst], func(t *testing.T) {
					t.Parallel()
					denial := &core.ProblemError{Status: status, Code: "pass_operation_unavailable"}
					database := core.DatabaseOperationError(io.ErrUnexpectedEOF)
					failure := errors.Join(denial, database)
					if sqlFirst {
						failure = errors.Join(database, denial)
					}
					ports := &operationReadPorts{fail: func(context.Context) error { return failure }}
					value, err := (interaction.RegistrationOperations{Ledger: ports, Domain: ports}).Read(
						t.Context(), "alice", derivedmutation.PassOperationQuery{},
					)
					require.ErrorIs(t, err, core.ErrDatabase)
					require.ErrorIs(t, err, denial)
					require.Equal(t, interaction.RegistrationOperationRead{}, value,
						"a prior authorized summary and its authorities must not escape as partial success")
					require.Equal(t, []string{"alice/first", "alice/second"}, ports.calls)
				})
			}
		})
	}
}

func TestRegistrationOperationsReadSkipsOrdinaryDenial(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			ports := &operationReadPorts{fail: func(context.Context) error {
				return &core.ProblemError{Status: status, Code: "pass_operation_unavailable"}
			}}
			value, err := (interaction.RegistrationOperations{Ledger: ports, Domain: ports}).Read(
				t.Context(), "alice", derivedmutation.PassOperationQuery{},
			)
			require.NoError(t, err)
			require.Len(t, value.Summaries, 2)
			require.Equal(t, "first", value.Summaries[0].ID)
			require.Equal(t, "third", value.Summaries[1].ID)
			require.NotEmpty(t, value.ReadAuthorities)
			require.Equal(t, []string{"alice/first", "alice/second", "alice/third"}, ports.calls)
		})
	}
}

func TestRegistrationOperationsReadPropagatesParentCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ports := &operationReadPorts{fail: func(current context.Context) error {
		cancel()
		return current.Err()
	}}
	value, err := (interaction.RegistrationOperations{Ledger: ports, Domain: ports}).Read(
		ctx, "alice", derivedmutation.PassOperationQuery{},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
	require.Equal(t, interaction.RegistrationOperationRead{}, value)
	require.Equal(t, []string{"alice/first", "alice/second"}, ports.calls)
}
