package interaction_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type registrationMenuDomain struct {
	interaction.RegistrationReadClient

	actor       passbooking.Booking
	invitations passbooking.InvitationPage
	queue       passbooking.BookingPage
	payments    passbooking.PaymentPage
	assignment  passbooking.AdminTarget
	takeover    passbooking.TakeoverTarget
	pageError   error
	bookingErr  error
	calls       []string
}

func (d *registrationMenuDomain) PassBooking(_ context.Context, owner, event string) (passbooking.Booking, error) {
	d.calls = append(d.calls, "booking:"+owner+":"+event)
	return d.actor, d.bookingErr
}

func (d *registrationMenuDomain) PassInvitations(
	_ context.Context,
	owner, event, cursor string,
) (passbooking.InvitationPage, error) {
	d.calls = append(d.calls, "invitations:"+owner+":"+event+":"+cursor)
	return d.invitations, d.pageError
}

func (d *registrationMenuDomain) PassQueue(
	_ context.Context,
	owner, event, cursor string,
) (passbooking.BookingPage, error) {
	d.calls = append(d.calls, "queue:"+owner+":"+event+":"+cursor)
	return d.queue, d.pageError
}

func (d *registrationMenuDomain) PassPaymentQueue(
	_ context.Context,
	owner, event, cursor string,
) (passbooking.PaymentPage, error) {
	d.calls = append(d.calls, "payments:"+owner+":"+event+":"+cursor)
	return d.payments, d.pageError
}

func (d *registrationMenuDomain) PassAdminTarget(
	_ context.Context,
	_, _ string,
	_ int64,
) (passbooking.AdminTarget, error) {
	return d.assignment, d.pageError
}

func (d *registrationMenuDomain) PassTakeoverTarget(
	_ context.Context,
	_, _ string,
	_ int64,
) (passbooking.TakeoverTarget, error) {
	return d.takeover, d.pageError
}

func TestRegistrationMenuInvitationObservedReadContinuity(t *testing.T) {
	t.Parallel()
	domain := &registrationMenuDomain{actor: passbooking.Booking{Owner: "actor", Event: "event", Version: 11},
		invitations: passbooking.InvitationPage{Next: "next", Invitations: []passbooking.Invitation{{
			From: passbooking.Contact{Owner: "inviter", Name: "Inviter"}, Version: 23,
		}}}}
	reader := interaction.RegistrationMenuReader{Domain: domain}
	manual, err := reader.Invitations(t.Context(), "actor", "event", "cursor", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"invitations:actor:event:cursor", "booking:actor:event"}, domain.calls)
	require.Equal(t, domain.invitations, manual.Page)
	require.Equal(t, passbooking.Command{Name: "accept", Event: "event", Version: 11,
		Target: "inviter", TargetVersion: 23}, manual.Entries[0].Accept)
	domain.calls = nil
	agent, err := reader.Invitations(t.Context(), "actor", "event", "cursor", &domain.actor)
	require.NoError(t, err)
	require.Equal(t, manual, agent)
	require.Equal(t, []string{"invitations:actor:event:cursor"}, domain.calls)
}

func TestRegistrationMenuQueueActionEligibility(t *testing.T) {
	t.Parallel()
	domain := &registrationMenuDomain{actor: passbooking.Booking{Version: 11}, queue: passbooking.BookingPage{
		Next: "next", Names: map[string]string{"paired": "Pair"}, Bookings: []passbooking.Booking{
			{Owner: "cancelled", Version: 21, State: "cancelled", Partner: "old"},
			{Owner: "solo", Version: 22, State: "assigned"},
			{Owner: "paired", Version: 23, State: "assigned", Partner: "partner"},
		}}}
	view, err := (interaction.RegistrationMenuReader{Domain: domain}).Queue(
		t.Context(),
		"actor",
		"event",
		"cursor",
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, domain.queue, view.Page)
	require.Nil(t, view.Entries[0].Cancel)
	require.Nil(t, view.Entries[0].Uncouple)
	require.NotNil(t, view.Entries[1].Cancel)
	require.Nil(t, view.Entries[1].Uncouple)
	require.Equal(t, passbooking.Command{Name: "admin_uncouple", Event: "event", Version: 11,
		Target: "paired", TargetVersion: 23}, *view.Entries[2].Uncouple)
}

func TestRegistrationMenuPaymentAttemptIdentity(t *testing.T) {
	t.Parallel()
	domain := &registrationMenuDomain{actor: passbooking.Booking{Version: 11}, payments: passbooking.PaymentPage{
		Next: "next", Items: []passbooking.PaymentReview{{Owner: "participant", TelegramID: 123,
			Payment: passbooking.Payment{Event: "event", Version: 37, Attempt: "attempt", Decision: "pending"}}},
	}}
	view, err := (interaction.RegistrationMenuReader{Domain: domain}).PaymentQueue(
		t.Context(),
		"actor",
		"event",
		"cursor",
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, domain.payments, view.Page)
	require.Equal(t, passbooking.Command{Name: "proof_accept", Event: "event", Version: 11,
		Target: "participant", TargetVersion: 37, PaymentAttempt: "attempt"}, view.Entries[0].Accept)
	require.Equal(t, "proof_reject", view.Entries[0].Reject.Name)
}

func TestRegistrationMenuAssignmentEligibility(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                               string
		canAssign, canCreate               bool
		version                            int64
		state                              string
		wantDraft, wantCommand, wantCreate bool
	}{
		{name: "denied", version: 23},
		{name: "existing", canAssign: true, version: 23, wantDraft: true, wantCommand: true},
		{name: "missing-profile", canAssign: true, wantDraft: true},
		{name: "create-profile", canAssign: true, canCreate: true, wantDraft: true, wantCommand: true, wantCreate: true},
		{name: "cancelled-profile", canAssign: true, canCreate: true, version: 23, state: "cancelled",
			wantDraft: true, wantCommand: true, wantCreate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			domain := &registrationMenuDomain{assignment: passbooking.AdminTarget{
				Booking: passbooking.Booking{
					Event:   "event",
					Owner:   "target",
					Version: test.version,
					State:   test.state,
				},
				ActorVersion:         11,
				ProfileVersion:       37,
				CanAssign:            test.canAssign,
				CanCreateFromProfile: test.canCreate,
			}}
			view, err := (interaction.RegistrationMenuReader{Domain: domain}).Assignment(
				t.Context(),
				"actor",
				"event",
				123,
				nil,
			)
			require.NoError(t, err)
			require.Equal(t, domain.assignment, view.Target)
			require.Equal(t, test.wantDraft, view.Draft != nil)
			require.Equal(t, test.wantCommand, view.Command != nil)
			if test.wantCreate {
				require.Equal(t, &passbooking.AdminCreate{FromProfile: true, ProfileVersion: 37}, view.Command.Create)
			}
		})
	}
}

func TestRegistrationMenuTakeoverEligibility(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, partner, paymentAdmin string
		backfill, takeover          bool
	}{
		{name: "already-own", paymentAdmin: "actor"},
		{name: "other-contact", paymentAdmin: "other", takeover: true},
		{name: "uncouple-own", paymentAdmin: "actor", partner: "partner", takeover: true},
		{name: "backfill-own", paymentAdmin: "actor", backfill: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			domain := &registrationMenuDomain{takeover: passbooking.TakeoverTarget{
				Booking: passbooking.Booking{
					Event:        "historical",
					Owner:        "target",
					Version:      23,
					PaymentAdmin: test.paymentAdmin,
					Partner:      test.partner,
				}, ActorVersion: 11, CanBackfill: test.backfill,
			}}
			view, err := (interaction.RegistrationMenuReader{Domain: domain}).Takeover(
				t.Context(),
				"actor",
				"historical",
				123,
			)
			require.NoError(t, err)
			require.Equal(t, test.takeover, view.Takeover != nil)
			require.Equal(t, test.backfill, view.ReceivedOnly != nil)
			if test.backfill {
				require.Equal(
					t,
					passbooking.Command{Name: passbooking.CommandReceivedOnly, Event: "historical", Version: 11,
						Target: "target", TargetVersion: 23},
					*view.ReceivedOnly,
				)
			}
		})
	}
}

func TestRegistrationMenuReadFailuresExposeNoPartialProjection(t *testing.T) {
	t.Parallel()
	denied := &core.ProblemError{Status: 403, Code: "forbidden"}
	domain := &registrationMenuDomain{invitations: passbooking.InvitationPage{Invitations: []passbooking.Invitation{{
		From: passbooking.Contact{Owner: "private"}, Version: 23,
	}}}, bookingErr: context.Canceled}
	reader := interaction.RegistrationMenuReader{Domain: domain}
	view, err := reader.Invitations(t.Context(), "actor", "event", "cursor", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, view)
	domain.calls, domain.pageError = nil, denied
	view, err = reader.Invitations(t.Context(), "actor", "event", "cursor", &passbooking.Booking{Version: 11})
	require.ErrorIs(t, err, denied)
	require.Zero(t, view)
	require.Equal(t, []string{"invitations:actor:event:cursor"}, domain.calls)
}
