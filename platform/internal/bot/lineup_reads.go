package bot

import (
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func (b *Bot) performLineupRead(query agent.LineupQuery, input *agent.Input) error {
	if input.LineupSource == nil || input.LineupSource.Remaining <= 0 {
		return errors.New("lineup read budget exhausted")
	}
	read, err := b.Lineup.Query(input.LineupSource.Now, query)
	if err != nil {
		return err
	}
	input.LineupSource.Remaining--
	for index, previous := range input.LineupSource.Reads {
		if previous.Scope == query.Scope {
			input.LineupSource.Reads[index] = read
			return nil
		}
	}
	input.LineupSource.Reads = append(input.LineupSource.Reads, read)
	return nil
}
