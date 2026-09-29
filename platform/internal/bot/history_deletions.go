package bot

import (
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
)

const historyDeleted = "history_deleted"

// Only a durably saved terminal plan permits the inbox to finish a stale update.
var errHistoryPlanTerminal = fmt.Errorf("terminal history plan: %w", appclient.ErrReadStale)
