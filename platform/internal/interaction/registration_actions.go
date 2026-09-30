package interaction

import "github.com/complynx/zns-chatbot/platform/internal/passbooking"

// RegistrationInvitationCommand binds the inviter's observed invitation version,
// separately from the acting user's booking version. Manual and agent admission
// use this same identity; execution still checks current domain authorization.
func RegistrationInvitationCommand(name, event string, actorVersion int64,
	invitation passbooking.Invitation,
) passbooking.Command {
	return passbooking.Command{Name: name, Event: event, Version: actorVersion,
		Target: invitation.From.Owner, TargetVersion: invitation.Version}
}

// RegistrationQueueCommand binds an administrator action to the observed booking.
func RegistrationQueueCommand(name, event string, actorVersion int64,
	booking passbooking.Booking,
) passbooking.Command {
	return passbooking.Command{Name: name, Event: event, Version: actorVersion,
		Target: booking.Owner, TargetVersion: booking.Version}
}

// RegistrationPaymentReviewCommand binds the exact payment attempt and version.
// A booking version cannot substitute for the version of the proof decision.
func RegistrationPaymentReviewCommand(name, event string, actorVersion int64,
	owner string, payment passbooking.Payment,
) passbooking.Command {
	return passbooking.Command{Name: name, Event: event, Version: actorVersion,
		Target: owner, TargetVersion: payment.Version, PaymentAttempt: payment.Attempt}
}

// RegistrationTakeoverCommand preserves the actor version supplied by the
// authorized takeover projection, including targets in historical events.
func RegistrationTakeoverCommand(name, event string, target passbooking.TakeoverTarget) passbooking.Command {
	return passbooking.Command{Name: name, Event: event, Version: target.ActorVersion,
		Target: target.Booking.Owner, TargetVersion: target.Booking.Version}
}

// RegistrationAssignmentDraft retains manual choices only for the same observed
// event, actor, target and profile. It never rebinds an admitted saved command;
// callers use it before assigning the durable operation key.
func RegistrationAssignmentDraft(event string, target passbooking.AdminTarget,
	previous *passbooking.AdminAssignment,
) passbooking.AdminAssignment {
	if previous != nil && previous.Event == event &&
		previous.Target == target.Booking.Owner && previous.TargetVersion == target.Booking.Version &&
		previous.Version == target.ActorVersion &&
		(previous.Create == nil || previous.Create.ProfileVersion == target.ProfileVersion) {
		return *previous
	}
	return passbooking.AdminAssignment{Event: event, Version: target.ActorVersion,
		Target: target.Booking.Owner, TargetVersion: target.Booking.Version}
}
