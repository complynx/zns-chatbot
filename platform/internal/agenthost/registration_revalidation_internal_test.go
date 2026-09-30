package agenthost

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type revalidationDomain struct {
	calls       []string
	booking     passbooking.Booking
	invitations []passbooking.Invitation
	failures    map[string]error
}

func (d *revalidationDomain) call(ctx context.Context, operation, owner, event string) error {
	d.calls = append(d.calls, operation+":"+owner+":"+event)
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.failures[operation]
}

func (d *revalidationDomain) PassCapabilities(
	ctx context.Context, owner, event string,
) (passbooking.Capabilities, error) {
	return passbooking.Capabilities{Event: event}, d.call(ctx, "capabilities", owner, event)
}

func (d *revalidationDomain) PassBooking(ctx context.Context, owner, event string) (passbooking.Booking, error) {
	return d.booking, d.call(ctx, "booking", owner, event)
}

func (d *revalidationDomain) PassAdminTarget(
	ctx context.Context, owner, event string, target int64,
) (passbooking.AdminTarget, error) {
	return passbooking.AdminTarget{}, d.call(ctx, "admin-"+strconv.FormatInt(target, 10), owner, event)
}

func (d *revalidationDomain) PassTakeoverTarget(
	ctx context.Context, owner, event string, target int64,
) (passbooking.TakeoverTarget, error) {
	return passbooking.TakeoverTarget{}, d.call(ctx, "takeover-"+strconv.FormatInt(target, 10), owner, event)
}

func (d *revalidationDomain) PassPaymentQueue(
	ctx context.Context, owner, event, cursor string,
) (passbooking.PaymentPage, error) {
	return passbooking.PaymentPage{}, d.call(ctx, "payments-"+cursor, owner, event)
}

func (d *revalidationDomain) PassQueue(
	ctx context.Context, owner, event, cursor string,
) (passbooking.BookingPage, error) {
	return passbooking.BookingPage{}, d.call(ctx, "queue-"+cursor, owner, event)
}

func (d *revalidationDomain) PassInvitations(
	ctx context.Context, owner, event, cursor string,
) (passbooking.InvitationPage, error) {
	return passbooking.InvitationPage{Invitations: d.invitations}, d.call(ctx, "invitations-"+cursor, owner, event)
}

type revalidationAuthority struct {
	owners  []string
	refs    [][]readsource.Authority
	failure error
}

func (a *revalidationAuthority) CheckReadAuthorities(
	_ context.Context, owner string, refs []readsource.Authority,
) error {
	a.owners = append(a.owners, owner)
	a.refs = append(a.refs, refs)
	return a.failure
}

func revalidationBooking() passbooking.Booking {
	return passbooking.Booking{Owner: "owner", Event: "a", Version: 7, State: "cancelled",
		CreatedAt: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)}
}

func TestRegistrationRevalidationRefreshOrderAndNoPayloadRestoration(t *testing.T) {
	t.Parallel()
	booking := revalidationBooking()
	domain := &revalidationDomain{booking: booking}
	value := &agent.RegistrationContext{Events: []passbooking.Event{{ID: "b"}, {ID: "a"}}, CurrentEvent: "b",
		Remaining: 1, Reads: []agent.RegistrationReadResult{
			{Request: agent.RegistrationProposal{Name: agent.RegistrationRead, Event: "a", View: "home"},
				Historical: true, Omitted: true},
			{Request: agent.RegistrationProposal{Name: agent.RegistrationRead, Event: "c",
				View: registrationInvitationsView, Cursor: "observed"}},
		}}
	host := RegistrationRevalidator{Domain: domain, Authority: &revalidationAuthority{}}
	require.NoError(t, host.Context(t.Context(), "owner", value))
	require.Equal(t, []string{"capabilities:owner:a", "capabilities:owner:b", "capabilities:owner:c",
		"booking:owner:a", "invitations-observed:owner:c"}, domain.calls)
	require.Len(t, value.Capabilities, 3)
	require.Equal(t, 1, value.Remaining)
	require.True(t, value.Reads[0].Omitted)
	require.Nil(t, value.Reads[0].Booking, "refresh cannot restore an omitted historical payload")
	require.Empty(t, value.Reads[0].Error, "cancelled historical owner booking retains read access")
	require.NoError(t, host.Context(t.Context(), "owner", nil))
	require.Len(t, domain.calls, 5)
}

func TestRegistrationRevalidationOwnerIdentityAndPermissionControls(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		current  passbooking.Booking
		failure  error
		expected string
	}{
		{name: "unchanged-cancelled", current: revalidationBooking()},
		{name: "missing", expected: registrationReadForbidden},
		{name: "denied", failure: &core.ProblemError{Status: 403, Code: "forbidden"}, expected: registrationReadForbidden},
		{name: "recreated", current: passbooking.Booking{Owner: "owner", Event: "a", Version: 7,
			CreatedAt: time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)}, expected: "stale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			previous := revalidationBooking()
			domain := &revalidationDomain{booking: test.current, failures: map[string]error{"booking": test.failure}}
			read := agent.RegistrationReadResult{Request: agent.RegistrationProposal{Event: "a", View: "home"},
				Booking: &previous, Next: "retained-cursor"}
			require.NoError(t, (RegistrationRevalidator{Domain: domain}).Read(t.Context(), "owner", &read))
			require.Equal(t, test.expected, read.Error)
			require.Equal(t, []string{"booking:owner:a"}, domain.calls)
			if test.expected != "" {
				require.Nil(t, read.Booking)
				require.Empty(t, read.Next)
			}
		})
	}
}

func TestRegistrationRevalidationSourceCheckPrecedesCurrentQueueRights(t *testing.T) {
	t.Parallel()
	for _, stale := range []bool{false, true} {
		t.Run("source", func(t *testing.T) {
			t.Parallel()
			booking := revalidationBooking()
			read := agent.RegistrationReadResult{Request: agent.RegistrationProposal{Event: "a", View: hostQueue},
				Queue: []passbooking.Booking{booking}, Next: "retained"}
			authority := &revalidationAuthority{}
			if stale {
				authority.failure = &core.ProblemError{Status: 409, Code: "history_stale"}
			}
			domain := &revalidationDomain{}
			err := (RegistrationRevalidator{Domain: domain, Authority: authority}).Read(t.Context(), "owner", &read)
			require.NoError(t, err)
			require.Equal(t, []string{"owner"}, authority.owners)
			require.Len(t, authority.refs[0], 2, "current role and exact queue booking are separate witnesses")
			require.Empty(t, authority.refs[0][0].Registration.Owner)
			require.Equal(t, agent.RegistrationAdminAssign, authority.refs[0][0].Registration.Action)
			require.Equal(t, "owner", authority.refs[0][1].Registration.Owner)
			if stale {
				require.Empty(t, domain.calls)
				require.Equal(t, registrationReadForbidden, read.Error)
				require.Nil(t, read.Queue)
			} else {
				require.Equal(t, []string{"queue-:owner:a"}, domain.calls)
				require.Equal(t, []passbooking.Booking{booking}, read.Queue)
			}
		})
	}
}

func TestRegistrationRevalidationViewQueriesAndFailureEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ view, operation string }{
		{view: hostQueue, operation: "queue-"}, {view: hostPaymentQueue, operation: "payments-"},
		{view: agent.RegistrationAdminTarget, operation: "admin-123"},
		{view: agent.RegistrationTakeoverTarget, operation: "takeover-123"},
		{view: registrationInvitationsView, operation: "invitations-cursor"},
	} {
		t.Run(test.view, func(t *testing.T) {
			t.Parallel()
			for _, failure := range []error{nil, &core.ProblemError{Status: 403, Code: "forbidden"},
				context.Canceled, context.DeadlineExceeded,
				core.DatabaseFailure(&core.ProblemError{Status: 404, Code: "not_found"})} {
				domain := &revalidationDomain{failures: map[string]error{test.operation: failure}}
				read := agent.RegistrationReadResult{Request: agent.RegistrationProposal{Event: "a", View: test.view,
					Target: "123", Cursor: "cursor"}, Next: "private-cursor"}
				err := (RegistrationRevalidator{Domain: domain, Authority: &revalidationAuthority{}}).
					Read(t.Context(), "owner", &read)
				switch {
				case failure == nil:
					require.NoError(t, err)
				case core.IsDatabaseFailure(failure) ||
					errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded):
					require.ErrorIs(t, err, failure)
					require.Empty(t, read.Error, "operational failures cannot become successful redaction")
				default:
					require.NoError(t, err)
					require.Equal(t, registrationReadForbidden, read.Error)
					require.Empty(t, read.Next)
				}
				require.Equal(t, []string{test.operation + ":owner:a"}, domain.calls)
			}
		})
	}
}

func TestRegistrationRevalidationInvitationCreationAndDependencyBounds(t *testing.T) {
	t.Parallel()
	created := revalidationBooking().CreatedAt
	old := passbooking.Invitation{From: passbooking.Contact{Owner: "inviter"}, Version: 4, CreatedAt: created}
	for _, current := range []passbooking.Invitation{
		{From: passbooking.Contact{Owner: "inviter"}, Version: 4, CreatedAt: created.Add(time.Second)},
		{From: passbooking.Contact{Owner: "other"}, Version: 4, CreatedAt: created},
		{From: passbooking.Contact{Owner: "inviter"}, Version: 5, CreatedAt: created},
	} {
		require.False(t, PassInvitationsPresent([]passbooking.Invitation{old}, []passbooking.Invitation{current}))
	}
	require.True(t, PassInvitationsPresent([]passbooking.Invitation{old}, []passbooking.Invitation{old}))
	require.False(
		t,
		PassInvitationsPresent([]passbooking.Invitation{{Version: 1}}, []passbooking.Invitation{{Version: 1}}),
	)
	domain := &revalidationDomain{invitations: []passbooking.Invitation{old}}
	host := RegistrationRevalidator{Domain: domain, Authority: &revalidationAuthority{}}
	dependency := interaction.PassContextDependency{Request: agent.RegistrationProposal{Event: "a",
		View: registrationInvitationsView, Cursor: "retained"}}
	changed, err := host.DependencyChanged(t.Context(), "owner", dependency)
	require.NoError(t, err)
	require.True(t, changed, "missing identities cannot authorize a retained invitation read")
	require.Empty(t, domain.calls)
	dependency.Invitations = []interaction.InvitationIdentity{{Owner: "inviter", Version: 4, CreatedAt: created}}
	changed, err = host.DependencyChanged(t.Context(), "owner", dependency)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, []string{"invitations-retained:owner:a"}, domain.calls)
	domain.invitations = nil
	changed, err = host.DependencyChanged(t.Context(), "owner", dependency)
	require.NoError(t, err)
	require.True(t, changed)
}

func TestRegistrationRevalidationCancellationAndMalformedEvidence(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	domain := &revalidationDomain{booking: revalidationBooking()}
	read := agent.RegistrationReadResult{Historical: true, Request: agent.RegistrationProposal{Event: "a"}}
	require.ErrorIs(t, (RegistrationRevalidator{Domain: domain}).Read(ctx, "owner", &read), context.Canceled)
	require.Empty(t, read.Error)
	read = agent.RegistrationReadResult{Request: agent.RegistrationProposal{Event: "a", View: hostQueue},
		Queue: []passbooking.Booking{{Owner: "target", Event: "a", Version: 3}}}
	require.NoError(t, (RegistrationRevalidator{Domain: domain}).Read(t.Context(), "owner", &read))
	require.Equal(t, registrationReadForbidden, read.Error)
	require.Len(t, domain.calls, 1, "incomplete creation identity stops before authority or domain I/O")
	changed, err := (RegistrationRevalidator{Domain: domain}).DependencyChanged(t.Context(), "owner",
		interaction.PassContextDependency{Request: agent.RegistrationProposal{View: hostQueue}})
	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, domain.calls, 1)
}
