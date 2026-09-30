package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (r *passMenuRenderer) adminAssignment(ctx context.Context) error {
	if r.state.AdminTargetTelegramID == 0 {
		if _, err := r.bot.API.PassQueue(ctx, r.owner, r.state.Event, ""); err != nil {
			return err
		}
		r.lines = append(r.lines, r.text(i18n.RegistrationAssignmentTarget))
		return nil
	}
	view, err := (interaction.RegistrationMenuReader{Domain: r.bot.API}).
		Assignment(ctx, r.owner, r.state.Event, r.state.AdminTargetTelegramID, r.state.Assignment)
	if err != nil {
		return err
	}
	target := view.Target
	r.lines = append(
		r.lines,
		passMenuLabel(target.Name),
		r.bookingText(target.Booking),
		r.text(i18n.RegistrationAssignmentHint),
	)
	if view.Draft == nil {
		return nil
	}
	r.state.Assignment = view.Draft
	if view.Command == nil {
		r.lines = append(r.lines, r.text(i18n.RegistrationAssignmentTarget))
		return nil
	}
	command := *view.Command
	r.assignmentChoices(command, target.CurrentTier)
	r.assignmentSummary(command)
	label := i18n.RegistrationAssignmentApply
	if command.Create != nil {
		label = i18n.RegistrationAssignmentCreateProfile
	}
	r.choices = append(
		r.choices,
		passMenuChoice{label: r.text(label), action: passMenuAction{Assignment: &command}},
	)
	return nil
}

func (r *passMenuRenderer) assignmentOption(label string, command passbooking.AdminAssignment) {
	state := r.state
	state.Assignment = &command
	r.choices = append(r.choices, passMenuChoice{label: label, action: passMenuAction{View: &state}})
}

func (r *passMenuRenderer) assignmentChoices(command passbooking.AdminAssignment, currentTier int) {
	standard := command
	standard.TotalPrice = nil
	r.assignmentOption(r.text(i18n.RegistrationAssignmentDefault), standard)
	free := command
	zero := 0
	free.TotalPrice = &zero
	r.assignmentOption(r.text(i18n.RegistrationAssignmentFree), free)
	for _, skip := range []bool{false, true} {
		choice := command
		choice.SkipBalance, choice.AppendTier = &skip, nil
		id := i18n.RegistrationAssignmentInclude
		if skip {
			id = i18n.RegistrationAssignmentExclude
		}
		r.assignmentOption(r.text(id), choice)
	}
	if currentTier > 0 {
		choice := command
		choice.AppendTier, choice.SkipBalance = &currentTier, nil
		label, _ := i18n.Translate(
			r.language,
			i18n.RegistrationAssignmentTier,
			map[string]string{"tier": strconv.Itoa(currentTier)},
		)
		r.assignmentOption(label, choice)
	}
}

func (r *passMenuRenderer) assignmentSummary(command passbooking.AdminAssignment) {
	price := r.text(i18n.RegistrationAssignmentDefault)
	if command.TotalPrice != nil {
		price = strconv.Itoa(*command.TotalPrice) + " RUB"
	}
	balance := "—"
	if command.SkipBalance != nil {
		balance = r.text(i18n.RegistrationAssignmentInclude)
		if *command.SkipBalance {
			balance = r.text(i18n.RegistrationAssignmentExclude)
		}
	}
	if command.AppendTier != nil {
		balance, _ = i18n.Translate(
			r.language,
			i18n.RegistrationAssignmentTier,
			map[string]string{"tier": strconv.Itoa(*command.AppendTier)},
		)
	}
	text, _ := i18n.Translate(
		r.language,
		i18n.RegistrationAssignmentDraft,
		map[string]string{passPriceParameter: price, "balance": balance},
	)
	r.lines = append(r.lines, text)
}
