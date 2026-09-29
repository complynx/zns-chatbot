package passbooking

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const pageSize = 25

type InvitationPage struct {
	Invitations []Invitation `json:"invitations"`
	Next        string       `json:"next"`
}

type BookingPage struct {
	Bookings []Booking         `json:"bookings"`
	Next     string            `json:"next"`
	Names    map[string]string `json:"names"`
}

// The cursor is an ordering boundary, never an authority or object reference.
// Keeping the value rather than looking up a row survives cancellation/deletion.
func pageCursor(at time.Time, telegramID int64) string {
	value := at.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(telegramID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func parsePageCursor(value string) (time.Time, int64, error) {
	if value == "" {
		return time.Time{}, 0, nil
	}
	const maxCursorBytes = 100
	if len(value) > maxCursorBytes {
		return time.Time{}, 0, invalid()
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, 0, invalid()
	}
	stamp, id, ok := strings.Cut(string(raw), "|")
	at, timeErr := time.Parse(time.RFC3339Nano, stamp)
	telegramID, idErr := strconv.ParseInt(id, 10, 64)
	if !ok || timeErr != nil || idErr != nil || telegramID <= 0 {
		return time.Time{}, 0, invalid()
	}
	return at, telegramID, nil
}
