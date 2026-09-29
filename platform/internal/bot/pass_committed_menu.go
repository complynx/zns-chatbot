package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// Only the host's actual committed outcome can replace command input lineage
// with a fresh domain menu. A proposal or a trusted failure notice cannot.
func (b *Bot) committedRegistrationMenu(
	ctx context.Context,
	owner string,
	update int64,
	plan interaction.SavedPlan,
	reply interaction.Reply,
) (interaction.RegistrationMenu, bool, error) {
	var event, action string
	if reply.Origin != interaction.TrustedReply {
		return interaction.RegistrationMenu{}, false, nil
	}
	switch {
	case plan.RegistrationCommand != nil:
		event, action = plan.RegistrationCommand.Event, plan.RegistrationCommand.Name
	case plan.RegistrationAssignment != nil:
		event, action = plan.RegistrationAssignment.Event, agent.RegistrationAdminAssign
	default:
		return interaction.RegistrationMenu{}, false, nil
	}
	var committed bool
	err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2
 AND kind='registration_action' AND content->>'committed'='true' AND content->>'event'=$3 AND content->>'action'=$4)`, owner, update, event, action).Scan(&committed)
	if err != nil || !committed {
		return interaction.RegistrationMenu{}, false, err
	}
	menu, err := b.freshRegistrationOutcomeMenu(ctx, owner, plan, event, action)
	return menu, err == nil, err
}

func (b *Bot) freshRegistrationOutcomeMenu(
	ctx context.Context,
	owner string,
	plan interaction.SavedPlan,
	event, action string,
) (interaction.RegistrationMenu, error) {
	menu := interaction.RegistrationMenu{Event: event, View: passMenuHome}
	switch action {
	case registrationProofAccept, registrationProofReject:
		menu.View = registrationPaymentQueue
	case agent.RegistrationAdminAssign:
		menu.View = passMenuQueue
		if plan.RegistrationMenu == nil || plan.RegistrationMenu.AdminTargetTelegramID <= 0 {
			return menu, nil
		}
		target, err := b.API.PassAdminTarget(ctx, owner, event, plan.RegistrationMenu.AdminTargetTelegramID)
		if err != nil {
			return menu, err
		}
		if target.Booking.Owner != plan.RegistrationAssignment.Target {
			return menu, errors.New("committed assignment target changed")
		}
		menu.View, menu.AdminTargetTelegramID = agent.RegistrationAdminTarget, target.Booking.TelegramID
	case passbooking.CommandTakeover, passbooking.CommandReceivedOnly:
		if plan.RegistrationMenu == nil || plan.RegistrationMenu.AdminTargetTelegramID <= 0 {
			return menu, errors.New("committed takeover target missing")
		}
		target, err := b.API.PassTakeoverTarget(ctx, owner, event, plan.RegistrationMenu.AdminTargetTelegramID)
		if err != nil {
			return menu, err
		}
		if target.Booking.Owner != plan.RegistrationCommand.Target {
			return menu, errors.New("committed takeover target changed")
		}
		menu.View, menu.AdminTargetTelegramID = agent.RegistrationTakeoverTarget, target.Booking.TelegramID
	}
	return menu, nil
}
