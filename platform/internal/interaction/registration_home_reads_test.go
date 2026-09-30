package interaction_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type registrationHomeDomain struct {
	interaction.RegistrationReadClient

	booking       passbooking.Booking
	payment       passbooking.Payment
	contacts      []passbooking.Contact
	bookingErr    error
	paymentErr    error
	paymentCancel context.CancelFunc
	contactErr    error
	calls         []string
}

func (d *registrationHomeDomain) PassEvents(_ context.Context, owner string) ([]passbooking.Event, error) {
	d.calls = append(d.calls, "events:"+owner)
	return []passbooking.Event{{ID: "active"}}, nil
}
func (d *registrationHomeDomain) PassBooking(_ context.Context, owner, event string) (passbooking.Booking, error) {
	d.calls = append(d.calls, "booking:"+owner+":"+event)
	return d.booking, d.bookingErr
}
func (d *registrationHomeDomain) PassPayment(
	_ context.Context, actor, event, owner string,
) (passbooking.Payment, error) {
	d.calls = append(d.calls, "payment:"+actor+":"+event+":"+owner)
	if d.paymentCancel != nil {
		d.paymentCancel()
	}
	return d.payment, d.paymentErr
}
func (d *registrationHomeDomain) PassPaymentAdmins(
	_ context.Context, owner, event string,
) ([]passbooking.Contact, error) {
	d.calls = append(d.calls, "contacts:"+owner+":"+event)
	return d.contacts, d.contactErr
}

func TestRegistrationContactSelectionDoesNotCreatePrivateContacts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		selection *interaction.RegistrationContactSelection
		selected  string
		reads     int
	}{
		{name: "agent-list", reads: 1},
		{name: "empty-manual", selection: &interaction.RegistrationContactSelection{}},
		{name: "booked-first", selection: &interaction.RegistrationContactSelection{
			BookingAdmin: "booked", PendingAdmin: "pending"}, selected: "booked", reads: 1},
		{name: "pending-only", selection: &interaction.RegistrationContactSelection{
			PendingAdmin: "pending"}, selected: "pending", reads: 1},
		{name: "missing-selected", selection: &interaction.RegistrationContactSelection{
			BookingAdmin: "absent", PendingAdmin: "pending"}, reads: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			domain := &registrationHomeDomain{contacts: []passbooking.Contact{
				{Owner: "booked", TelegramID: 11}, {Owner: "pending", TelegramID: 23},
			}}
			view, err := (interaction.RegistrationHomeReader{Domain: domain}).
				Contacts(t.Context(), "actor", "event", test.selection)
			require.NoError(t, err)
			require.Len(t, domain.calls, test.reads)
			if test.reads == 0 {
				require.Zero(t, view)
				return
			}
			require.Equal(t, []string{"contacts:actor:event"}, domain.calls)
			require.Equal(t, domain.contacts, view.Contacts)
			if test.selected == "" {
				require.Nil(t, view.Selected)
			} else {
				require.Equal(t, test.selected, view.Selected.Owner)
			}
		})
	}
}

func TestRegistrationHistoricalScopeAndReadOrder(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, view, state string
		version           int64
		denied, showPay   bool
		paymentReads      int
	}{
		{name: "empty-home", version: 11, state: "paid", showPay: true},
		{name: "home-assigned", view: "home", version: 11, state: "assigned", showPay: true},
		{name: "home-cancelled", view: "home", version: 11, state: "cancelled"},
		{name: "payment", view: "payment", version: 11, state: "paid", paymentReads: 1},
		{name: "missing-booking", view: "payment", denied: true},
		{name: "queue-denied", view: "queue", version: 11, denied: true},
		{name: "profile-denied", view: "profile", version: 11, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			domain := &registrationHomeDomain{booking: passbooking.Booking{
				Owner: "actor", Event: "historical", Version: test.version, State: test.state,
			}, payment: passbooking.Payment{Event: "historical", Version: 23, Attempt: "attempt"}}
			value, err := (interaction.RegistrationHomeReader{Domain: domain}).
				Historical(t.Context(), "actor", "historical", test.view)
			if test.denied {
				var problem *core.ProblemError
				require.ErrorAs(t, err, &problem)
				require.Equal(t, 403, problem.Status)
				require.Equal(t, "forbidden", problem.Code)
				require.Zero(t, value)
			} else {
				require.NoError(t, err)
				require.Equal(t, domain.booking, value.Booking)
				require.Equal(t, test.showPay, value.ShowPayment)
			}
			expected := []string{"booking:actor:historical"}
			if test.paymentReads != 0 {
				expected = append(expected, "payment:actor:historical:actor")
				require.Equal(t, &domain.payment, value.Payment)
			}
			require.Equal(t, expected, domain.calls)
		})
	}
}

func TestRegistrationHistoricalPaymentAbsenceIsExact(t *testing.T) {
	t.Parallel()
	missing := &core.ProblemError{Status: 404, Code: "pass_payment_missing"}
	for _, test := range []struct {
		name   string
		err    error
		absent bool
	}{
		{name: "missing", err: missing, absent: true},
		{name: "different-missing", err: &core.ProblemError{Status: 404, Code: "other_missing"}},
		{name: "permission", err: &core.ProblemError{Status: 403, Code: "pass_payment_missing"}},
		{name: "positive-sql", err: core.DatabaseFailure(missing)},
		{name: "cancellation", err: context.Canceled},
		{name: "joined-cancellation", err: errors.Join(missing, context.Canceled)},
		{name: "joined-deadline", err: errors.Join(missing, context.DeadlineExceeded)},
		{name: "provider-outage", err: errors.New("provider unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			domain := &registrationHomeDomain{paymentErr: test.err}
			value, err := (interaction.RegistrationHomeReader{Domain: domain}).
				HistoricalPayment(t.Context(), "actor", "historical")
			if test.absent {
				require.NoError(t, err)
				require.True(t, value.Absent)
				require.Nil(t, value.Payment)
			} else {
				require.ErrorIs(t, err, test.err)
				require.Zero(t, value)
			}
			require.Equal(t, []string{"payment:actor:historical:actor"}, domain.calls)
		})
	}
}

func TestRegistrationHistoricalSQLFailureIsNotAnAdmittedRead(t *testing.T) {
	t.Parallel()
	domain := &registrationHomeDomain{booking: passbooking.Booking{Owner: "actor", Event: "historical", Version: 11},
		paymentErr: core.DatabaseFailure(&core.ProblemError{Status: 404, Code: "pass_payment_missing"})}
	ledger := &registrationReadLedger{}
	reader := interaction.RegistrationReader{Domain: domain, Store: ledger}
	input := agent.Input{Registration: &agent.RegistrationContext{Remaining: 1}}
	err := reader.Read(t.Context(), "actor", 1, agent.RegistrationProposal{
		Name: agent.RegistrationRead, Event: "historical", View: "payment",
	}, &input, "Read my past payment")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Empty(t, ledger.completed)
	require.Equal(
		t,
		[]string{"events:actor", "booking:actor:historical", "payment:actor:historical:actor"},
		domain.calls,
	)
}

func TestRegistrationHomeReadErrorsExposeZeroProjection(t *testing.T) {
	t.Parallel()
	domain := &registrationHomeDomain{contacts: []passbooking.Contact{{Owner: "private"}}, contactErr: context.Canceled}
	reader := interaction.RegistrationHomeReader{Domain: domain}
	contacts, err := reader.Contacts(t.Context(), "actor", "event", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, contacts)
	domain.booking = passbooking.Booking{Owner: "actor", Version: 11}
	domain.paymentErr = context.DeadlineExceeded
	value, err := reader.Historical(t.Context(), "actor", "event", "payment")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, value)
}

func TestRegistrationHistoricalFailedReadsKeepReservation(t *testing.T) {
	t.Parallel()
	missing := &core.ProblemError{Status: 404, Code: "pass_payment_missing"}
	for _, test := range []struct {
		name   string
		err    error
		cancel bool
		want   error
	}{
		{name: "missing", err: missing},
		{name: "joined-canceled", err: errors.Join(missing, context.Canceled), want: context.Canceled},
		{name: "joined-deadline", err: errors.Join(missing, context.DeadlineExceeded), want: context.DeadlineExceeded},
		{name: "caller-canceled", err: missing, cancel: true, want: context.Canceled},
		{name: "caller-canceled-success", cancel: true, want: context.Canceled},
		{name: "sql-missing", err: core.DatabaseFailure(missing), want: core.ErrDatabase},
		{name: "sql-caller-canceled", err: core.DatabaseFailure(missing), cancel: true, want: core.ErrDatabase},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			domain := &registrationHomeDomain{
				booking:    passbooking.Booking{Owner: "actor", Event: "historical", Version: 11},
				paymentErr: test.err,
			}
			if test.cancel {
				domain.paymentCancel = cancel
			}
			ledger := &registrationReadLedger{}
			input := agent.Input{Registration: &agent.RegistrationContext{Remaining: 1}}
			err := (interaction.RegistrationReader{Domain: domain, Store: ledger}).Read(ctx, "actor", 1,
				agent.RegistrationProposal{Name: agent.RegistrationRead, Event: "historical", View: "payment"},
				&input, "Read my past payment")
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
				require.Empty(t, ledger.completed)
				require.Equal(t, []string{"actor"}, ledger.owners)
			} else {
				require.NoError(t, err)
				require.Len(t, ledger.completed, 1)
				require.Empty(t, ledger.completed[0].Error)
			}
			require.Equal(t, []string{
				"events:actor", "booking:actor:historical", "payment:actor:historical:actor",
			}, domain.calls)
		})
	}
}

func TestRegistrationHistoricalCallerCancellationHasZeroProjection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	domain := &registrationHomeDomain{paymentCancel: cancel,
		paymentErr: &core.ProblemError{Status: 404, Code: "pass_payment_missing"}}
	value, err := (interaction.RegistrationHomeReader{Domain: domain}).HistoricalPayment(ctx, "actor", "historical")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, value)
}
