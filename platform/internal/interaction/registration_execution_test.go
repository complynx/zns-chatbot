package interaction_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type registrationCall struct {
	kind    string
	owner   string
	command any
	source  *readsource.Derivation
}

type registrationPorts struct {
	calls      []registrationCall
	booking    passbooking.Booking
	assignment passbooking.AdminAssignmentResult
	batch      []passbooking.RuntimeBatchItem
	err        error
	found      bool
}

func (p *registrationPorts) capture(kind, owner string, command any, source *readsource.Derivation) {
	call := registrationCall{kind: kind, owner: owner, command: command}
	if source != nil {
		cloned := source.Clone()
		call.source = &cloned
		*source.Generation = 999
	}
	p.calls = append(p.calls, call)
}

func (p *registrationPorts) ExecutePassBooking(_ context.Context, owner string,
	command passbooking.Command,
) (passbooking.Booking, error) {
	p.capture("manual-command", owner, command, nil)
	return p.booking, p.err
}

func (p *registrationPorts) AssignPass(_ context.Context, owner string,
	command passbooking.AdminAssignment,
) (passbooking.AdminAssignmentResult, error) {
	p.capture("manual-assignment", owner, command, nil)
	return p.assignment, p.err
}

func (p *registrationPorts) RunPassBatch(_ context.Context, owner string,
	command passbooking.RuntimeBatch,
) ([]passbooking.RuntimeBatchItem, error) {
	p.capture("manual-batch", owner, command, nil)
	return p.batch, p.err
}

func (p *registrationPorts) ExecuteDerivedPassBooking(_ context.Context, owner string,
	command passbooking.Command, source readsource.Derivation,
) (passbooking.Booking, error) {
	p.capture("derived-command", owner, command, &source)
	return p.booking, p.err
}

func (p *registrationPorts) AssignDerivedPass(_ context.Context, owner string,
	command passbooking.AdminAssignment, source readsource.Derivation,
) (passbooking.AdminAssignmentResult, error) {
	p.capture("derived-assignment", owner, command, &source)
	return p.assignment, p.err
}

func (p *registrationPorts) RunDerivedPassBatch(_ context.Context, owner string,
	command passbooking.RuntimeBatch, source readsource.Derivation,
) ([]passbooking.RuntimeBatchItem, error) {
	p.capture("derived-batch", owner, command, &source)
	return p.batch, p.err
}

func (p *registrationPorts) PassBookingReceipt(_ context.Context, owner string,
	command passbooking.Command, source readsource.Derivation,
) (derivedmutation.Receipt[passbooking.Booking], error) {
	p.capture("command-receipt", owner, command, &source)
	return derivedmutation.Receipt[passbooking.Booking]{Found: p.found, Result: p.booking}, p.err
}

func (p *registrationPorts) PassAssignmentReceipt(_ context.Context, owner string,
	command passbooking.AdminAssignment, source readsource.Derivation,
) (derivedmutation.Receipt[passbooking.AdminAssignmentResult], error) {
	p.capture("assignment-receipt", owner, command, &source)
	return derivedmutation.Receipt[passbooking.AdminAssignmentResult]{Found: p.found, Result: p.assignment}, p.err
}

func executionSource() readsource.Derivation {
	generation := int64(17)
	return readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
}

func TestRegistrationExecutionRoutesBoundValues(t *testing.T) {
	t.Parallel()
	for _, derived := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "derived"}[derived], func(t *testing.T) {
			t.Parallel()
			ports := &registrationPorts{
				booking:    passbooking.Booking{Version: 8},
				assignment: passbooking.AdminAssignmentResult{AssignedCount: 2},
				err:        errors.New("original application failure"),
			}
			executor := interaction.RegistrationExecutor{Manual: ports, Derived: ports}
			var source *readsource.Derivation
			if derived {
				value := executionSource()
				source = &value
			}
			command := passbooking.Command{Key: "original-command", Event: "event", Name: "cancel", Version: 7}
			booking, err := executor.Command(t.Context(), "actor", command, source)
			require.Equal(t, ports.booking, booking)
			require.ErrorIs(t, err, ports.err)
			assignment := passbooking.AdminAssignment{Key: "original-assignment", Event: "event", TargetVersion: 3}
			assigned, err := executor.Assignment(t.Context(), "actor", assignment, source)
			require.Equal(t, ports.assignment, assigned)
			require.ErrorIs(t, err, ports.err)
			require.Len(t, ports.calls, 2)
			require.Equal(t, command, ports.calls[0].command)
			require.Equal(t, assignment, ports.calls[1].command)
			for _, call := range ports.calls {
				require.Equal(t, "actor", call.owner)
				require.Equal(t, source, call.source)
			}
			if derived {
				require.Equal(t, int64(17), *source.Generation)
				require.Equal(t, "derived-command", ports.calls[0].kind)
				require.Equal(t, "derived-assignment", ports.calls[1].kind)
			} else {
				require.Equal(t, "manual-command", ports.calls[0].kind)
				require.Equal(t, "manual-assignment", ports.calls[1].kind)
			}
		})
	}
}

func TestRegistrationBatchKeepsPartialOutcomesWithoutMetadata(t *testing.T) {
	t.Parallel()
	ports := &registrationPorts{batch: []passbooking.RuntimeBatchItem{
		{TelegramID: 1, Outcome: passbooking.AdminBatchOutcome{Status: "succeeded"}},
		{TelegramID: 2, Outcome: passbooking.AdminBatchOutcome{Status: "not_attempted", Code: "interrupted"}},
	}}
	executor := interaction.RegistrationExecutor{Manual: ports, Derived: ports,
		Writer: func(context.Context, string, interaction.RegistrationExecutionRecord) error {
			t.Fatal("batch must not invent a scalar committed record")
			return nil
		},
	}
	command := passbooking.RuntimeBatch{Key: "original-batch", Event: "event", Recipients: []int64{1, 2}}
	source := executionSource()
	for _, evidence := range []*readsource.Derivation{nil, &source} {
		result, err := executor.Batch(t.Context(), "actor", command, evidence)
		require.NoError(t, err)
		require.Equal(t, ports.batch, result)
	}
	require.Len(t, ports.calls, 2)
	require.Equal(t, command, ports.calls[0].command)
	require.Equal(t, command, ports.calls[1].command)
	require.Equal(t, source, *ports.calls[1].source)
	require.Equal(t, int64(17), *source.Generation)
}

func TestRegistrationExecutionRecordsOnlyDurableOutcomes(t *testing.T) {
	t.Parallel()
	refusal := &core.ProblemError{Status: http.StatusConflict, Code: "stale"}
	for _, test := range []struct {
		name      string
		err       error
		committed string
	}{
		{name: "success", committed: "true"},
		{name: "refusal", err: refusal, committed: "false"},
		{name: "infrastructure", err: errors.New("database unavailable")},
		{name: "server", err: &core.ProblemError{Status: http.StatusServiceUnavailable}},
		{name: "cancelled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "joined cancellation", err: errors.Join(refusal, context.Canceled)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ports := &registrationPorts{err: test.err, booking: passbooking.Booking{Version: 9}}
			var records []interaction.RegistrationExecutionRecord
			executor := interaction.RegistrationExecutor{Manual: ports,
				Writer: func(_ context.Context, owner string, record interaction.RegistrationExecutionRecord) error {
					require.Equal(t, "actor", owner)
					require.Len(t, ports.calls, 1)
					records = append(records, record)
					return nil
				},
			}
			result, err := executor.Command(t.Context(), "actor",
				passbooking.Command{Key: "key", Name: "cancel", Event: "event"}, nil)
			require.Equal(t, ports.booking, result)
			require.ErrorIs(t, err, test.err)
			if test.committed == "" {
				require.Empty(t, records)
				return
			}
			require.Equal(t, []interaction.RegistrationExecutionRecord{
				{Action: "cancel", Event: "event", Committed: test.committed},
			}, records)
		})
	}
}

func TestRegistrationRecordFailureRetainsBothCausesAndResult(t *testing.T) {
	t.Parallel()
	refusal := &core.ProblemError{Status: http.StatusConflict, Code: "stale"}
	writeErr := errors.New("interaction unavailable")
	ports := &registrationPorts{err: refusal, booking: passbooking.Booking{Version: 4}}
	executor := interaction.RegistrationExecutor{Manual: ports,
		Writer: func(context.Context, string, interaction.RegistrationExecutionRecord) error { return writeErr },
	}
	result, err := executor.Command(t.Context(), "actor", passbooking.Command{Key: "exact"}, nil)
	require.Equal(t, ports.booking, result)
	require.ErrorIs(t, err, refusal)
	require.ErrorIs(t, err, writeErr)
	require.ErrorIs(t, err, interaction.ErrRegistrationExecutionRecord)
}

func TestRegistrationRecoveryAfterMetadataFailureUsesSavedReceiptOnly(t *testing.T) {
	t.Parallel()
	ports := &registrationPorts{booking: passbooking.Booking{Version: 8}, found: true}
	source := executionSource()
	command := passbooking.Command{Key: "persisted-exact-key", Name: "cancel", Event: "event", Version: 7}
	writeErr := errors.New("interaction write failed after domain commit")
	executor := interaction.RegistrationExecutor{Derived: ports,
		Writer: func(context.Context, string, interaction.RegistrationExecutionRecord) error { return writeErr },
	}
	result, err := executor.Command(t.Context(), "actor", command, &source)
	require.ErrorIs(t, err, writeErr)
	require.Equal(t, ports.booking, result)
	saved, err := json.Marshal(interaction.SavedPlan{RegistrationCommand: &command})
	require.NoError(t, err)
	var restored interaction.SavedPlan
	require.NoError(t, json.Unmarshal(saved, &restored))
	var record interaction.RegistrationExecutionRecord
	// No execution ports are available after restart: recovery cannot execute.
	restarted := interaction.RegistrationExecutor{Receipts: ports,
		Writer: func(_ context.Context, _ string, value interaction.RegistrationExecutionRecord) error {
			record = value
			return nil
		},
	}
	receipt, err := restarted.RecoverCommand(t.Context(), "actor", *restored.RegistrationCommand, source)
	require.NoError(t, err)
	require.True(t, receipt.Found)
	require.Equal(t, result, receipt.Result)
	require.Len(t, ports.calls, 2)
	require.Equal(t, "derived-command", ports.calls[0].kind)
	require.Equal(t, "command-receipt", ports.calls[1].kind)
	require.Equal(t, command, ports.calls[1].command)
	require.Equal(t, source, *ports.calls[1].source)
	require.Equal(t, "true", record.Committed)
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	require.JSONEq(t, `{"action":"cancel","event":"event","committed":"true"}`, string(raw))
}

func TestRegistrationReceiptStatesAndWriteFailureRemainDistinct(t *testing.T) {
	t.Parallel()
	denied := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	writeErr := errors.New("metadata unavailable")
	for _, test := range []struct {
		name  string
		found bool
		err   error
	}{
		{name: "missing"},
		{name: "denied", err: denied},
		{name: "unknown", err: context.DeadlineExceeded},
		{name: "found with metadata failure", found: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ports := &registrationPorts{found: test.found, err: test.err,
				assignment: passbooking.AdminAssignmentResult{AssignedCount: 2}}
			writes := 0
			executor := interaction.RegistrationExecutor{Receipts: ports,
				Writer: func(context.Context, string, interaction.RegistrationExecutionRecord) error {
					writes++
					return writeErr
				},
			}
			command := passbooking.AdminAssignment{Key: "saved-assignment", Event: "event", TargetVersion: 7}
			receipt, err := executor.RecoverAssignment(t.Context(), "actor", command, executionSource())
			require.Equal(t, test.found, receipt.Found)
			require.Equal(t, ports.assignment, receipt.Result)
			require.Len(t, ports.calls, 1)
			require.Equal(t, command, ports.calls[0].command)
			if test.found {
				require.Equal(t, 1, writes)
				require.ErrorIs(t, err, writeErr)
				require.ErrorIs(t, err, interaction.ErrRegistrationExecutionRecord)
			} else {
				require.Zero(t, writes)
				require.ErrorIs(t, err, test.err)
			}
		})
	}
}
