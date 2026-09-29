package migrate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

type OrderDependency struct {
	Source     string              `json:"source"`
	Legacy     UserLegacyReference `json:"legacy"`
	TelegramID int64               `json:"telegram_id,omitempty"`
	EventID    string              `json:"event_id,omitempty"`
}

func readOrderDependencies(
	root *os.Root,
	entry File,
	manifest Manifest,
	limits Limits,
	plan *OrderPlan,
	budget io.Writer,
) error {
	collection := ""
	for _, source := range manifest.Coverage {
		if source.Domain == entry.Source {
			collection = source.Name
		}
	}
	rows := OrderPlan{BotID: manifest.BotID}
	if err := readOrderPlanFile(root, entry, collection, limits, &rows, budget); err != nil {
		return err
	}
	for _, row := range rows.Records {
		var fields map[string]json.RawMessage
		if json.Unmarshal(row.Record, &fields) != nil {
			return errors.New("order_dependency_invalid")
		}
		dependency := OrderDependency{Source: entry.Source, Legacy: row.Legacy}
		if entry.Source == usersSource {
			bot, ok := telegramNumber(fields["bot_id"])
			if !ok || bot != manifest.BotID {
				continue
			}
			if dependency.TelegramID, ok = telegramNumber(fields["user_id"]); !ok {
				return errors.New("order_dependency_invalid")
			}
		} else if json.Unmarshal(fields["key"], &dependency.EventID) != nil ||
			!tokenPattern.MatchString(dependency.EventID) {
			return errors.New("order_dependency_invalid")
		}
		plan.Dependencies = append(plan.Dependencies, dependency)
	}
	return nil
}
