package interaction_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type registrationBatchInputMemory struct {
	saved     *passbooking.RuntimeBatch
	winner    *passbooking.RuntimeBatch
	attempted passbooking.RuntimeBatch
	loadErr   error
	saveErr   error
	loads     int
	saves     int
	owners    []string
	updates   []int64
}

func (s *registrationBatchInputMemory) LoadBatchInput(_ context.Context, owner string,
	updateID int64,
) (passbooking.RuntimeBatch, bool, error) {
	s.loads++
	s.owners, s.updates = append(s.owners, owner), append(s.updates, updateID)
	if s.loadErr != nil {
		return passbooking.RuntimeBatch{}, false, s.loadErr
	}
	if s.saved == nil {
		return passbooking.RuntimeBatch{}, false, nil
	}
	return *s.saved, true, nil
}
func (s *registrationBatchInputMemory) SaveBatchInput(_ context.Context, owner string,
	updateID int64, command passbooking.RuntimeBatch,
) error {
	s.saves++
	s.attempted = command
	s.owners, s.updates = append(s.owners, owner), append(s.updates, updateID)
	if s.winner != nil {
		s.saved = s.winner
	} else {
		s.saved = &command
	}
	return s.saveErr
}

type registrationBatchEvents struct {
	items  []passbooking.Event
	err    error
	owners []string
}

func (d *registrationBatchEvents) PassEvents(_ context.Context, owner string) ([]passbooking.Event, error) {
	d.owners = append(d.owners, owner)
	return d.items, d.err
}

func TestRegistrationBatchSavedInputResumesBeforeInterpretation(t *testing.T) {
	t.Parallel()
	stored := passbooking.RuntimeBatch{Key: "original-key", Event: "old-event", Action: "admin_cancel",
		Recipients: []int64{11, 23}}
	store := &registrationBatchInputMemory{saved: &stored}
	result, err := (interaction.RegistrationBatchAdmission{Store: store, Now: func(context.Context) (time.Time, error) {
		t.Fatal("durable retry must not observe time")
		return time.Time{}, context.Canceled
	}}).
		Manual(t.Context(), "actor", 37, func() (passbooking.RuntimeBatch, error) {
			t.Fatal("saved input must not be parsed or rebound")
			return passbooking.RuntimeBatch{}, nil
		})
	require.NoError(t, err)
	require.Equal(t, stored, result)
	require.Equal(t, 1, store.loads)
	require.Zero(t, store.saves)
	require.Equal(t, []string{"actor"}, store.owners)
	require.Equal(t, []int64{37}, store.updates)
}

func TestRegistrationBatchAdmissionReloadsConcurrentWinner(t *testing.T) {
	t.Parallel()
	winner := passbooking.RuntimeBatch{Key: "winner-key", Event: "winner-event", Action: "admin_cancel",
		Recipients: []int64{99}}
	store := &registrationBatchInputMemory{winner: &winner}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	events := &registrationBatchEvents{items: []passbooking.Event{
		{ID: "default-event", SalesStart: &started, OpenEnded: true},
	}}
	result, err := (interaction.RegistrationBatchAdmission{Store: store, Events: events,
		Now: func(context.Context) (time.Time, error) { return now, nil }}).Manual(t.Context(), "actor", 37,
		func() (passbooking.RuntimeBatch, error) {
			return passbooking.RuntimeBatch{Action: "admin_cancel", Recipients: []int64{11}}, nil
		})
	require.NoError(t, err)
	require.Equal(t, winner, result)
	require.Equal(t, 2, store.loads)
	require.Equal(t, 1, store.saves)
	require.Equal(t, "telegram-pass-batch-37", store.attempted.Key)
	require.Equal(t, "default-event", store.attempted.Event)
	require.Equal(t, []string{"actor"}, events.owners)
	require.Equal(t, []int64{37, 37, 37}, store.updates)
}

func TestRegistrationBatchDurableAdmissionSurvivesInterruptedAcknowledgement(t *testing.T) {
	t.Parallel()
	store := &registrationBatchInputMemory{saveErr: context.Canceled}
	coordinator := interaction.RegistrationBatchAdmission{Store: store}
	decode := func() (passbooking.RuntimeBatch, error) {
		return passbooking.RuntimeBatch{Event: "event", Action: "admin_assign", Recipients: []int64{11}}, nil
	}
	result, err := coordinator.Manual(t.Context(), "actor", 37, decode)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, result)
	store.saveErr = nil
	result, err = coordinator.Manual(t.Context(), "actor", 37, func() (passbooking.RuntimeBatch, error) {
		t.Fatal("interrupted acknowledgement must reload the existing admission")
		return passbooking.RuntimeBatch{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "telegram-pass-batch-37", result.Key)
	require.Equal(t, "event", result.Event)
	require.Equal(t, []int64{11}, result.Recipients)
	require.Equal(t, 1, store.saves)
}

func TestRegistrationBatchAdmissionFailureDoesNotSave(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		loadErr   error
		decodeErr error
		eventErr  error
		wantErr   error
	}{
		{name: "load", loadErr: context.Canceled, wantErr: context.Canceled},
		{name: "decode", decodeErr: context.Canceled, wantErr: context.Canceled},
		{name: "events", eventErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
		{name: "empty-events", wantErr: interaction.ErrRegistrationBatchEvent},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &registrationBatchInputMemory{loadErr: test.loadErr}
			events := &registrationBatchEvents{err: test.eventErr}
			result, err := (interaction.RegistrationBatchAdmission{Store: store, Events: events}).
				Manual(t.Context(), "actor", 37, func() (passbooking.RuntimeBatch, error) {
					return passbooking.RuntimeBatch{Action: "admin_cancel", Recipients: []int64{11}}, test.decodeErr
				})
			require.ErrorIs(t, err, test.wantErr)
			require.Zero(t, result)
			require.Zero(t, store.saves)
		})
	}
}

func TestRegistrationGroundedBatchRetainsExistingHostKeyBoundary(t *testing.T) {
	t.Parallel()
	input := agent.Input{Registration: &agent.RegistrationContext{Events: []passbooking.Event{{ID: "event"}}}}
	price := 100
	recipients := []int64{11, 23}
	command, err := interaction.BindGroundedRegistrationBatch("assign 11 and 23", input,
		agent.RegistrationAdminAssign, "event", recipients, &agent.RegistrationAssignment{TotalPrice: &price})
	require.NoError(t, err)
	require.Empty(t, command.Key)
	require.Equal(t, []int64{11, 23}, command.Recipients)
	recipients[0] = 99
	require.Equal(t, []int64{11, 23}, command.Recipients)
	require.Equal(t, &price, command.Options.TotalPrice)
	require.Empty(t, command.Options.Key)
	require.Empty(t, command.Options.Target)
	require.Zero(t, command.Options.Version)
}

func TestRegistrationGroundedBatchRejectsUnsupportedEvidence(t *testing.T) {
	t.Parallel()
	input := agent.Input{Registration: &agent.RegistrationContext{Events: []passbooking.Event{{ID: "event"}}}}
	name := "Unmentioned"
	for _, test := range []struct {
		name, event, evidence, action string
		recipients                    []int64
		options                       *agent.RegistrationAssignment
	}{
		{name: "unknown-event", event: "other", evidence: "11", action: "admin_cancel", recipients: []int64{11}},
		{name: "foreign-recipient", event: "event", evidence: "11", action: "admin_cancel", recipients: []int64{23}},
		{name: "no-recipient", event: "event", action: "admin_cancel"},
		{name: "cancel-options", event: "event", evidence: "11", action: "admin_cancel",
			recipients: []int64{11}, options: &agent.RegistrationAssignment{}},
		{name: "invented-name", event: "event", evidence: "11", action: agent.RegistrationAdminAssign,
			recipients: []int64{11}, options: &agent.RegistrationAssignment{Create: true, Role: "leader", LegalName: &name}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command, err := interaction.BindGroundedRegistrationBatch(test.evidence, input,
				test.action, test.event, test.recipients, test.options)
			require.Error(t, err)
			require.Zero(t, command)
		})
	}
}

func TestRegistrationBatchClockSelectsAfterSalesBoundary(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := start.Add(time.Hour)
	events := &registrationBatchEvents{
		items: []passbooking.Event{
			{ID: "first", SalesStart: &start, OpenEnded: true},
			{ID: "second", SalesStart: &later, OpenEnded: true},
		},
	}
	for _, action := range []string{"admin_assign", "admin_cancel", "admin_uncouple", "tier"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			store := &registrationBatchInputMemory{}
			observed := later.Add(time.Microsecond)
			coordinator := interaction.RegistrationBatchAdmission{
				Store:  store,
				Events: &registrationBatchEvents{items: events.items},
				Now:    func(context.Context) (time.Time, error) { return observed, nil },
			}
			result, err := coordinator.Manual(t.Context(), "actor", 37, func() (passbooking.RuntimeBatch, error) {
				return passbooking.RuntimeBatch{Action: action, Recipients: []int64{11}}, nil
			})
			require.NoError(t, err)
			require.Equal(t, "second", result.Event)
			require.Equal(t, "second", store.attempted.Event)
		})
	}
}
func TestRegistrationBatchClockFailureDoesNotSave(t *testing.T) {
	t.Parallel()
	store := &registrationBatchInputMemory{}
	events := &registrationBatchEvents{items: []passbooking.Event{{ID: "event", OpenEnded: true}}}
	result, err := (interaction.RegistrationBatchAdmission{Store: store, Events: events, Now: func(context.Context) (time.Time, error) { return time.Time{}, context.Canceled }}).Manual(
		t.Context(),
		"actor",
		37,
		func() (passbooking.RuntimeBatch, error) {
			return passbooking.RuntimeBatch{Action: "admin_cancel", Recipients: []int64{11}}, nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, result)
	require.Zero(t, store.saves)
}
