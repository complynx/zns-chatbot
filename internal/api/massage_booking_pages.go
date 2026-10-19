package api

import (
	"encoding/json"
	"fmt"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Reserve 4 KiB of the advertised 64 KiB for the protocol envelope.
const massagePageResultLimit = 60 << 10

// Fit the largest prefix and continue after its last ID. The database cursor is
// retained when all records fit, including when further database pages exist.
func boundedMassagePage(page db.MassageBookingPage) (db.MassageBookingPage, error) {
	for count := len(page.Bookings); count > 0; count-- {
		result := db.MassageBookingPage{Bookings: page.Bookings[:count], NextAfter: page.NextAfter}
		if count < len(page.Bookings) {
			result.NextAfter = &result.Bookings[count-1].ID
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return db.MassageBookingPage{}, err
		}
		if len(raw) <= massagePageResultLimit {
			return result, nil
		}
	}
	if len(page.Bookings) == 0 {
		return page, nil
	}
	return db.MassageBookingPage{}, fmt.Errorf("stored massage booking exceeds list response bound")
}
