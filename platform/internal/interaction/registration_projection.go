package interaction

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const registrationEventLimit = 20

// RegistrationContextBytes bounds the registration data supplied to the model.
const RegistrationContextBytes = 20 * 1024

// RegistrationEvents returns the next bounded page of event labels.
func RegistrationEvents(events []passbooking.Event, cursor string) ([]passbooking.Event, string, error) {
	offset := 0
	if cursor != "" {
		value, err := strconv.Atoi(cursor)
		if err != nil || value < 0 {
			return nil, "", errors.New("invalid event cursor")
		}
		offset = min(value, len(events))
	}
	last := min(offset+registrationEventLimit, len(events))
	page := append([]passbooking.Event{}, events[offset:last]...)
	for index := range page {
		titles := map[string]string{}
		for _, language := range []string{"en", "ru"} {
			titles[language] = RegistrationLabel(page[index].Titles[language])
		}
		page[index].Titles = titles
		short := map[string]string{}
		for _, language := range []string{"en", "ru"} {
			short[language] = RegistrationLabel(page[index].Title(language, true))
		}
		page[index].ShortTitles = short
		page[index].CountryEmoji = RegistrationLabel(page[index].CountryEmoji)
	}
	next := ""
	if last < len(events) {
		next = strconv.Itoa(last)
	}
	return page, next, nil
}

// BoundRegistrationContext monotonically omits reads until model context fits.
func BoundRegistrationContext(value *agent.RegistrationContext) error {
	for index := range value.Reads {
		data, err := json.Marshal(value)
		if err == nil && len(data) <= RegistrationContextBytes {
			return nil
		}
		read := &value.Reads[index]
		*read = agent.RegistrationReadResult{Request: read.Request, Error: "context_budget", Omitted: true}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > RegistrationContextBytes {
		return errors.New("registration context exceeds budget")
	}
	return nil
}

// RegistrationLabel applies the shared Unicode-safe display bound.
func RegistrationLabel(value string) string {
	const maximum = 60
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum]) + "…"
	}
	return value
}
