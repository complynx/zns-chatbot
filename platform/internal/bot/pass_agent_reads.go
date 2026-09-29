package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func (b *Bot) performRegistrationRead(
	ctx context.Context,
	owner string,
	id int64,
	p agent.RegistrationProposal,
	input *agent.Input,
) error {
	return (interaction.RegistrationReader{Domain: b.API, Store: b.readStore()}).Read(
		ctx,
		owner,
		id,
		p,
		input,
		agenthost.CurrentRequestEvidence(*input),
	)
}
