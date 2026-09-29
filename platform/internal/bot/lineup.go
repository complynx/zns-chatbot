package bot

import (
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func (b *Bot) addLineupContext(input *agent.Input) {
	input.LineupSource = b.Lineup.Snapshot(time.Now())
}
