package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
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
