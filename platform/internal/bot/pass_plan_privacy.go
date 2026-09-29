package bot

import (
	"errors"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
)

var errPassPlanTerminal = fmt.Errorf("terminal registration plan: %w", appclient.ErrReadStale)
var errPassAuthorityUnavailable = errors.New("registration authority unavailable")

func (b *Bot) planAuthorization() agenthost.PlanAuthorization {
	return agenthost.PlanAuthorization{
		DB:              b.DB,
		Sources:         botScriptAuthority{bot: b},
		Scripts:         b.scriptHost().Store,
		Terminal:        errPassPlanTerminal,
		HistoryTerminal: errHistoryPlanTerminal,
		Unavailable:     errPassAuthorityUnavailable,
	}
}
