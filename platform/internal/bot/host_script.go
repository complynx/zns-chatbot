package bot

import (
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
)

func (b *Bot) scriptHost() agenthost.ScriptHost {
	return agenthost.ScriptHost{
		Worker:           b.Scripts,
		RefreshKnowledge: b.refreshScriptKnowledge,
		Store: agenthost.ScriptStore{
			Reads:      b.readStore(),
			DB:         b.DB,
			Policy:     agenthost.ScriptAuthorization{ScriptDomainAuthority: botScriptAuthority{bot: b}},
			StaleError: appclient.ErrReadStale,
		},
		Registry:       agenthost.ScriptRegistry{Catalog: botScriptCatalog{bot: b}},
		ReadLimitError: appclient.ErrReadLimit,
	}
}

func (b *Bot) readStore() agenthost.ReadStore {
	return agenthost.ReadStore{DB: b.DB, Policy: botScriptAuthority{bot: b}, Memory: b.Host}
}
