package agenthost

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const registrationReadForbidden = "forbidden"

// RegistrationRevalidationClient performs current owner-authorized domain reads.
type RegistrationRevalidationClient interface {
	PassCapabilities(context.Context, string, string) (passbooking.Capabilities, error)
	PassBooking(context.Context, string, string) (passbooking.Booking, error)
	PassAdminTarget(context.Context, string, string, int64) (passbooking.AdminTarget, error)
	PassTakeoverTarget(context.Context, string, string, int64) (passbooking.TakeoverTarget, error)
	PassPaymentQueue(context.Context, string, string, string) (passbooking.PaymentPage, error)
	PassQueue(context.Context, string, string, string) (passbooking.BookingPage, error)
	PassInvitations(context.Context, string, string, string) (passbooking.InvitationPage, error)
}

// RegistrationRevalidator owns current exposure of retained registration reads.
// Domain operations keep authorization; Authority validates exact source leaves.
type RegistrationRevalidator struct {
	Domain    RegistrationRevalidationClient
	Authority SourceAuthority
}

// Context rechecks the current projection without restoring omitted payloads or read budget.
func (h RegistrationRevalidator) Context(
	ctx context.Context,
	owner string,
	value *agent.RegistrationContext,
) error {
	if value == nil {
		return nil
	}
	if err := h.refreshCapabilities(ctx, owner, value); err != nil {
		return err
	}
	for index := range value.Reads {
		if err := h.Read(ctx, owner, &value.Reads[index]); err != nil {
			return err
		}
	}
	return interaction.BoundRegistrationContext(value)
}

func (h RegistrationRevalidator) Read(ctx context.Context, owner string, read *agent.RegistrationReadResult) error {
	if err := h.reauthorizeTarget(ctx, owner, read); err != nil || read.Error != "" {
		return err
	}
	if read.Historical || read.Booking != nil && read.Booking.Version > 0 {
		if err := h.reauthorizeOwner(ctx, owner, read); err != nil || read.Error != "" {
			return err
		}
	}
	var err error
	switch read.Request.View {
	case agent.RegistrationTakeoverTarget:
		id, parseErr := strconv.ParseInt(read.Request.Target, 10, 64)
		if parseErr != nil {
			return parseErr
		}
		_, err = h.Domain.PassTakeoverTarget(ctx, owner, read.Request.Event, id)
	case agent.RegistrationAdminTarget:
		id, parseErr := strconv.ParseInt(read.Request.Target, 10, 64)
		if parseErr != nil {
			return parseErr
		}
		_, err = h.Domain.PassAdminTarget(ctx, owner, read.Request.Event, id)
	case hostPaymentQueue:
		_, err = h.Domain.PassPaymentQueue(ctx, owner, read.Request.Event, "")
	case hostQueue:
		_, err = h.Domain.PassQueue(ctx, owner, read.Request.Event, "")
	case registrationInvitationsView:
		var page passbooking.InvitationPage
		page, err = h.Domain.PassInvitations(ctx, owner, read.Request.Event, read.Request.Cursor)
		if err == nil && !PassInvitationsPresent(read.Invitations, page.Invitations) {
			*read = agent.RegistrationReadResult{Request: read.Request, Error: "invitation_changed"}
		}
	}
	if err != nil && registrationRevalidationFailure(err) == nil {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: registrationReadForbidden}
		return nil
	}
	return err
}

// Every load used as model context must recheck current privileges. The stored
// snapshot preserves the read budget; it never preserves permission to view it.

func (h RegistrationRevalidator) reauthorizeTarget(
	ctx context.Context,
	owner string,
	read *agent.RegistrationReadResult,
) error {
	if read.AdminTarget != nil || read.TakeoverTarget != nil || PassQueueView(read.Request.View) {
		if !PassQueueEvidenceComplete(*read) {
			*read = agent.RegistrationReadResult{Request: read.Request, Error: registrationReadForbidden}
			return nil
		}
		dependencies := ScriptPassContext(
			&agent.RegistrationContext{Reads: []agent.RegistrationReadResult{*read}},
		)
		authorities, err := PassContextReadAuthorities(dependencies)
		if err != nil {
			return err
		}
		changed, err := ReadAuthoritiesChanged(ctx, h.Authority, owner, authorities)
		if err != nil {
			return err
		}
		if changed {
			*read = agent.RegistrationReadResult{Request: read.Request, Error: registrationReadForbidden}
			return nil
		}
	}
	return nil
}

func (h RegistrationRevalidator) refreshCapabilities(
	ctx context.Context,
	owner string,
	value *agent.RegistrationContext,
) error {
	events := []string{}
	for _, event := range value.Events {
		events = append(events, event.ID)
	}
	if value.CurrentEvent != "" {
		events = append(events, value.CurrentEvent)
	}
	for _, read := range value.Reads {
		if read.Request.Event != "" {
			events = append(events, read.Request.Event)
		}
	}
	slices.Sort(events)
	value.Capabilities = nil
	for _, event := range slices.Compact(events) {
		capability, err := h.Domain.PassCapabilities(ctx, owner, event)
		if err != nil {
			return err
		}
		value.Capabilities = append(value.Capabilities, capability)
	}
	return nil
}

func (h RegistrationRevalidator) reauthorizeOwner(
	ctx context.Context,
	owner string,
	read *agent.RegistrationReadResult,
) error {
	booking, err := h.Domain.PassBooking(ctx, owner, read.Request.Event)
	if err != nil && registrationRevalidationFailure(err) != nil {
		return err
	}
	if err != nil || booking.Version == 0 {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: registrationReadForbidden}
	} else if read.Booking != nil && !PassIdentity(*read.Booking).Matches(booking) {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: "stale"}
	}
	return nil
}

// Keep authority identity, never another copy of private booking content.

func (h RegistrationRevalidator) DependencyChanged(
	ctx context.Context,
	owner string,
	dependency interaction.PassContextDependency,
) (bool, error) {
	if PassQueueView(dependency.Request.View) && dependency.QueueAuthorities == nil {
		return true, nil
	}
	if dependency.TargetBooking != nil || PassQueueView(dependency.Request.View) {
		authorities, err := PassContextReadAuthorities([]interaction.PassContextDependency{dependency})
		if err != nil {
			return false, err
		}
		changed, err := ReadAuthoritiesChanged(ctx, h.Authority, owner, authorities)
		if changed || err != nil {
			return changed, err
		}
	}
	if dependency.Request.View == registrationInvitationsView && dependency.Invitations == nil {
		return true, nil
	}
	read := agent.RegistrationReadResult{Request: dependency.Request}
	if identity := dependency.Booking; identity != nil {
		read.Booking = &passbooking.Booking{
			Event:     identity.Event,
			Owner:     identity.Owner,
			Version:   identity.Version,
			CreatedAt: identity.CreatedAt,
		}
	}
	for _, identity := range dependency.Invitations {
		read.Invitations = append(
			read.Invitations,
			passbooking.Invitation{
				From:      passbooking.Contact{Owner: identity.Owner},
				Version:   identity.Version,
				CreatedAt: identity.CreatedAt,
			},
		)
	}
	if err := h.Read(ctx, owner, &read); err != nil {
		return false, err
	}
	return read.Error != "", nil
}

// PassInvitationsPresent retains exact creation identity across invitation refreshes.
func PassInvitationsPresent(previous, current []passbooking.Invitation) bool {
	for _, before := range previous {
		found := false
		for _, after := range current {
			if before.From.Owner == after.From.Owner && before.Version == after.Version &&
				!before.CreatedAt.IsZero() && before.CreatedAt.Equal(after.CreatedAt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func registrationRevalidationFailure(err error) error {
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	return err
}
