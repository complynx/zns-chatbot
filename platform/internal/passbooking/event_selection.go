package passbooking

import "time"

// ClosestEvent matches Python: latest started sale, otherwise earliest future sale.
// Input order breaks equal-start ties, including events without a tier start.
func ClosestEvent(events []Event, now time.Time) string {
	var chosen *Event
	for i := range events {
		e := &events[i]
		if !e.OpenEnded && !now.Before(e.FinishesAt) {
			continue
		}
		if chosen == nil {
			chosen = e
			continue
		}
		start, previous := eventSaleStart(*e), eventSaleStart(*chosen)
		started := !start.After(now)
		previousStarted := !previous.After(now)
		if started && (!previousStarted || start.After(previous)) ||
			!started && !previousStarted && start.Before(previous) {
			chosen = e
		}
	}
	if chosen == nil {
		return ""
	}
	return chosen.ID
}

func eventSaleStart(event Event) time.Time {
	if event.SalesStart != nil {
		return *event.SalesStart
	}
	value, _ := time.Parse(time.RFC3339Nano, "9999-12-31T23:59:59.999999Z")
	return value
}
