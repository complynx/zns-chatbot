package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
)

// botContextSources adapts the existing domain leaves, not model orchestration.
// C2/C3 can replace these leaves without changing the host's assembly policy.
type botContextSources struct{ bot *Bot }

func (b *Bot) contextBuilder() agenthost.ContextBuilder {
	return agenthost.ContextBuilder{
		Sources: botContextSources{
			bot: b,
		},
		History:     b.historyReader(),
		Knowledge:   b.knowledgeReader(),
		Reads:       b.API,
		PendingFood: foodPendingReader{db: b.DB},
		Lineup:      b.Lineup,
		OrderEvent:  b.currentOrderEvent(),
	}
}

func (s botContextSources) Registration(
	ctx context.Context, owner string, id int64, partners []int64,
) (*agent.RegistrationContext, error) {
	input := agent.Input{}
	err := s.bot.addRegistrationContext(ctx, owner, id, partners, &input)
	return input.Registration, err
}

func (s botContextSources) RefreshRegistration(
	ctx context.Context,
	owner string,
	value *agent.RegistrationContext,
) error {
	return s.bot.registrationRevalidator().Context(ctx, owner, value)
}
func (s botContextSources) Script(ctx context.Context, owner string, id int64) (*agent.ScriptContext, error) {
	input := agent.Input{}
	err := s.bot.scriptHost().AddContext(ctx, owner, id, &input)
	return input.Script, err
}
func (b *Bot) historyReader() agenthost.HistoryReader {
	return agenthost.HistoryReader{Domain: b.API, Summaries: b.Host, Authority: b.Host,
		Store: b.readStore(), Model: b.Model, Limit: b.HistoryLimit}
}

func (b *Bot) knowledgeReader() agenthost.KnowledgeReader {
	return agenthost.KnowledgeReader{Domain: b.API, Store: b.readStore()}
}

func (b *Bot) registrationRevalidator() agenthost.RegistrationRevalidator {
	return agenthost.RegistrationRevalidator{Domain: b.API, Authority: b.Host}
}
