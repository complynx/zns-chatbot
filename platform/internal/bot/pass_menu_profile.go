package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func registrationProfileFailure(err error) bool {
	problem, ok := errors.AsType[*core.ProblemError](err)
	if !ok {
		return false
	}
	switch problem.Code {
	case "pass_profile_required", "pass_identity_required", "pass_role_required":
		return true
	default:
		return false
	}
}

func (r *passMenuRenderer) registrationChoices(ctx context.Context, booking passbooking.Booking) error {
	profile, err := r.bot.API.PassProfile(ctx, r.owner)
	if err != nil {
		return err
	}
	events, err := r.bot.API.PassEvents(ctx, r.owner)
	if err != nil {
		return err
	}
	identityRequired := false
	for _, event := range events {
		if event.ID == r.state.Event {
			identityRequired = event.PassportRequired
		}
	}
	newBooking := booking.Version == 0 || booking.State == passStateCancelled
	missingRole := newBooking && profile.Role != profileLeader && profile.Role != profileFollower
	missingName, missingDocument := identityRequired &&
		profile.LegalName == "", identityRequired &&
		profile.Passport == ""
	if missingRole || missingName || missingDocument {
		r.registrationProfileGuidance(profile, missingRole, missingName, missingDocument)
		return nil
	}
	if r.state.Notice == i18n.RegistrationProfileRequired {
		r.state.Notice = ""
	}
	r.command(i18n.RegistrationSolo, passbooking.Command{Name: "solo", Event: r.state.Event,
		Version: booking.Version, PaymentAdmin: r.state.PaymentAdmin})
	r.navigate(i18n.RegistrationInvite, passInvite)
	return nil
}

func (r *passMenuRenderer) registrationProfileGuidance(profile passes.Profile, role, name, document bool) {
	if r.state.Notice != i18n.RegistrationProfileRequired {
		r.lines = append(r.lines, r.text(i18n.RegistrationProfileRequired))
	}
	for _, field := range []struct {
		missing    bool
		id, button i18n.ID
		field      string
	}{
		{name, i18n.RegistrationNeedName, i18n.ProfileSetName, profileLegalName},
		{document, i18n.RegistrationNeedDocument, i18n.RegistrationIdentityDocument, profilePassportField},
	} {
		if field.missing {
			r.lines = append(r.lines, r.text(field.id))
			if !profile.Frozen {
				r.profileCommand(
					field.button,
					passes.Command{
						Name:    profileBegin,
						Field:   field.field,
						Version: profile.Version,
						Origin:  originManual,
					},
				)
			}
		}
	}
	if profile.Frozen && (name || document) {
		r.lines = append(r.lines, r.text(i18n.ProfileFrozen))
	}
	if role {
		r.lines = append(r.lines, r.text(i18n.RegistrationNeedRole))
		for _, choice := range []struct {
			id    i18n.ID
			value string
		}{
			{i18n.RegistrationLeader, profileLeader}, {i18n.RegistrationFollower, profileFollower},
		} {
			r.profileCommand(
				choice.id,
				passes.Command{
					Name:    profileSet,
					Field:   profileRoleField,
					Value:   choice.value,
					Version: profile.Version,
					Origin:  originManual,
				},
			)
		}
	}
}
