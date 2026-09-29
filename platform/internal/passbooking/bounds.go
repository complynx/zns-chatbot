package passbooking

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const maxCatalogEvents = 100

// All command identifiers use the existing event and idempotency-key byte limit.
func boundedCommandStrings(c Command) bool {
	const maxIdentifierBytes = 200
	for _, value := range []string{c.Name, c.Event, c.Key, c.Target, c.PaymentAdmin, c.ProofID, c.PaymentAttempt} {
		if len(value) > maxIdentifierBytes || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func collectCatalog(rows pgx.Rows) ([]Event, error) {
	defer rows.Close()
	events := []Event{}
	var oversized bool
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Titles, &event.FinishesAt, &event.PassportRequired,
			&event.SalesStart, &event.ShortTitles, &event.CountryEmoji, &event.OpenEnded, &oversized); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(events) > maxCatalogEvents {
		return nil, conflict("pass_event_limit")
	}
	if oversized {
		return nil, core.ReadProblem("read_result_limit")
	}
	// The SQL budget bounds materialization; this also counts JSON escaping and framing.
	data, err := json.Marshal(events)
	if err != nil {
		return nil, err
	}
	if len(data) > core.ReadResourceBytes {
		return nil, core.ReadProblem("read_result_limit")
	}
	return events, nil
}
