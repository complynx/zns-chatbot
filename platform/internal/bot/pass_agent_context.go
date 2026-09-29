package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) addRegistrationContext(
	ctx context.Context,
	owner string,
	id int64,
	partners []int64,
	input *agent.Input,
) error {
	reader := interaction.RegistrationReader{Domain: b.API, Store: b.readStore(),
		Menu: func(ctx context.Context, owner string) (interaction.RegistrationMenu, error) {
			state, _, err := b.passMenuState(ctx, owner)
			return state, err
		}}
	value, err := reader.Context(ctx, owner, id, partners)
	if err != nil {
		return err
	}
	input.Registration = value
	return nil
}

// Recheck the current projection without restoring omitted payloads or read budget.
func (b *Bot) reauthorizeRegistrationContext(
	ctx context.Context,
	owner string,
	value *agent.RegistrationContext,
) error {
	if value == nil {
		return nil
	}
	if err := b.refreshPassCapabilities(ctx, owner, value); err != nil {
		return err
	}
	for index := range value.Reads {
		if err := b.reauthorizeRegistrationRead(ctx, owner, &value.Reads[index]); err != nil {
			return err
		}
	}
	return interaction.BoundRegistrationContext(value)
}

func (b *Bot) reauthorizeRegistrationRead(ctx context.Context, owner string, read *agent.RegistrationReadResult) error {
	if err := b.reauthorizePassTargetSnapshot(ctx, owner, read); err != nil || read.Error != "" {
		return err
	}
	if read.Historical || read.Booking != nil && read.Booking.Version > 0 {
		if err := b.reauthorizeOwnerPass(ctx, owner, read); err != nil || read.Error != "" {
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
		_, err = b.API.PassTakeoverTarget(ctx, owner, read.Request.Event, id)
	case agent.RegistrationAdminTarget:
		id, parseErr := strconv.ParseInt(read.Request.Target, 10, 64)
		if parseErr != nil {
			return parseErr
		}
		_, err = b.API.PassAdminTarget(ctx, owner, read.Request.Event, id)
	case registrationPaymentQueue:
		_, err = b.API.PassPaymentQueue(ctx, owner, read.Request.Event, "")
	case passMenuQueue:
		_, err = b.API.PassQueue(ctx, owner, read.Request.Event, "")
	case passMenuInvitations:
		var page passbooking.InvitationPage
		page, err = b.API.PassInvitations(ctx, owner, read.Request.Event, read.Request.Cursor)
		if err == nil && !passInvitationsPresent(read.Invitations, page.Invitations) {
			*read = agent.RegistrationReadResult{Request: read.Request, Error: "invitation_changed"}
		}
	}
	if err != nil && passMenuFailure(err) == nil {
		*read = agent.RegistrationReadResult{Request: read.Request, Error: mediaForbidden}
		return nil
	}
	return err
}

func registrationContacts(message *telegram.Message) []int64 {
	ids := []int64{}
	if message == nil {
		return ids
	}
	if message.Contact != nil && message.Contact.UserID > 0 {
		ids = append(ids, message.Contact.UserID)
	}
	origin := message.ForwardOrigin
	if origin != nil && origin.Type == "user" && origin.SenderUser != nil && !origin.SenderUser.IsBot &&
		origin.SenderUser.ID > 0 {
		ids = append(ids, origin.SenderUser.ID)
	}
	return ids
}

// Every load used as model context must recheck current privileges. The stored
// snapshot preserves the read budget; it never preserves permission to view it.

func (b *Bot) reauthorizePassTargetSnapshot(
	ctx context.Context,
	owner string,
	read *agent.RegistrationReadResult,
) error {
	if read.AdminTarget != nil || read.TakeoverTarget != nil || agenthost.PassQueueView(read.Request.View) {
		if !agenthost.PassQueueEvidenceComplete(*read) {
			*read = agent.RegistrationReadResult{Request: read.Request, Error: mediaForbidden}
			return nil
		}
		dependencies := agenthost.ScriptPassContext(
			&agent.RegistrationContext{Reads: []agent.RegistrationReadResult{*read}},
		)
		authorities, err := agenthost.PassContextReadAuthorities(dependencies)
		if err != nil {
			return err
		}
		changed, err := b.readAuthoritiesChanged(ctx, owner, authorities)
		if err != nil {
			return err
		}
		if changed {
			*read = agent.RegistrationReadResult{Request: read.Request, Error: mediaForbidden}
			return nil
		}
	}
	return nil
}
