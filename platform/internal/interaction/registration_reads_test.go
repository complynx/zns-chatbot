package interaction_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type registrationReadDomain struct {
	interaction.RegistrationReadClient

	failure error
	owners  []string
}

func (d *registrationReadDomain) PassEvents(_ context.Context, owner string) ([]passbooking.Event, error) {
	d.owners = append(d.owners, owner)
	return []passbooking.Event{{ID: "dance"}}, d.failure
}

type registrationReadLedger struct {
	owners    []string
	completed []agent.RegistrationReadResult
	reloaded  []agent.RegistrationReadResult
}

func (s *registrationReadLedger) ReserveRegistration(
	_ context.Context,
	owner string,
	_ int64,
	_ agent.RegistrationProposal,
) (int, error) {
	s.owners = append(s.owners, owner)
	return 0, nil
}

func (s *registrationReadLedger) CompleteRegistration(
	_ context.Context,
	owner string,
	_ int64,
	_ int,
	value agent.RegistrationReadResult,
) error {
	s.owners = append(s.owners, owner)
	s.completed = append(s.completed, value)
	return nil
}

func (s *registrationReadLedger) Registration(
	_ context.Context,
	owner string,
	_ int64,
) ([]agent.RegistrationReadResult, error) {
	s.owners = append(s.owners, owner)
	return s.reloaded, nil
}

func TestRegistrationReaderRequiresGroundingBeforeReservation(t *testing.T) {
	t.Parallel()
	ledger := &registrationReadLedger{}
	reader := interaction.RegistrationReader{Store: ledger}
	input := agent.Input{Registration: &agent.RegistrationContext{Remaining: 1}}
	err := reader.Read(t.Context(), "owner", 1, agent.RegistrationProposal{
		Name: agent.RegistrationRead, View: agent.RegistrationAdminTarget, Event: "dance", Target: "101",
	}, &input, "Synthetic private assignment input")
	require.EqualError(t, err, "administrator target lacks evidence")
	require.Empty(t, ledger.owners)
}

func TestRegistrationReaderCancellationRetainsReservation(t *testing.T) {
	t.Parallel()
	domain := &registrationReadDomain{failure: context.Canceled}
	ledger := &registrationReadLedger{}
	reader := interaction.RegistrationReader{Domain: domain, Store: ledger}
	input := agent.Input{Registration: &agent.RegistrationContext{Remaining: 1}}
	err := reader.Read(t.Context(), "owner", 1, agent.RegistrationProposal{
		Name: agent.RegistrationRead, View: "events",
	}, &input, "List events")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"owner"}, domain.owners)
	require.Equal(t, []string{"owner"}, ledger.owners)
	require.Empty(t, ledger.completed)
}

func TestRegistrationReaderUsesCurrentAuthorizedReload(t *testing.T) {
	t.Parallel()
	request := agent.RegistrationProposal{Name: agent.RegistrationRead, View: "events"}
	domain := &registrationReadDomain{}
	ledger := &registrationReadLedger{reloaded: []agent.RegistrationReadResult{
		{Request: request, Error: "forbidden", Omitted: true},
	}}
	reader := interaction.RegistrationReader{Domain: domain, Store: ledger}
	input := agent.Input{Registration: &agent.RegistrationContext{Remaining: 1}}
	require.NoError(t, reader.Read(t.Context(), "owner", 1, request, &input, "List events"))
	require.Len(t, ledger.completed, 1)
	require.Len(t, ledger.completed[0].Events, 1)
	require.Equal(t, ledger.reloaded, input.Registration.Reads)
	require.Empty(t, input.Registration.Reads[0].Events)
	require.Equal(t, agent.MaxRegistrationReads-1, input.Registration.Remaining)
	require.Equal(t, []string{"owner", "owner", "owner"}, ledger.owners)
}

func TestRegistrationReaderContextKeepsHintsSeparateFromReadAuthority(t *testing.T) {
	t.Parallel()
	domain := &registrationReadDomain{}
	ledger := &registrationReadLedger{reloaded: []agent.RegistrationReadResult{{Omitted: true, Error: "forbidden"}}}
	reader := interaction.RegistrationReader{Domain: domain, Store: ledger,
		Menu: func(_ context.Context, _ string) (interaction.RegistrationMenu, error) {
			return interaction.RegistrationMenu{Event: "dance", View: "invite", AdminTargetTelegramID: 101}, nil
		}}
	partners := []int64{202}
	value, err := reader.Context(t.Context(), "owner", 5, partners)
	require.NoError(t, err)
	partners[0] = 303
	require.Equal(t, []int64{202}, value.TrustedPartnerIDs)
	require.True(t, value.PendingPartner)
	require.Equal(t, ledger.reloaded, value.Reads)
	require.Equal(t, agent.MaxRegistrationReads-1, value.Remaining)
	require.Equal(t, []string{"owner"}, ledger.owners)
}

func TestRegistrationReaderContextCancellationStopsBeforeRetainedReads(t *testing.T) {
	t.Parallel()
	ledger := &registrationReadLedger{}
	reader := interaction.RegistrationReader{Domain: &registrationReadDomain{}, Store: ledger,
		Menu: func(_ context.Context, _ string) (interaction.RegistrationMenu, error) {
			return interaction.RegistrationMenu{}, context.Canceled
		}}
	value, err := reader.Context(t.Context(), "owner", 5, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, value)
	require.Empty(t, ledger.owners)
}
