package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// EventConfiguration retains source presentation, delivery and default pricing.
// AmountCapPerRole is source metadata: Python has no active reader for this cap.
type EventConfiguration struct {
	ShortTitles      map[string]string `json:"short_titles"`
	CountryEmoji     string            `json:"country_emoji"`
	ThreadChannel    string            `json:"thread_channel"`
	ThreadID         *int64            `json:"thread_id"`
	ThreadLocale     string            `json:"thread_locale"`
	DefaultPrice     *int32            `json:"default_price"`
	AmountCapPerRole int32             `json:"amount_cap_per_role"`
	OpenEnded        bool              `json:"open_ended"`
}

func eventConfiguration(record map[string]json.RawMessage, id string) (EventConfiguration, error) {
	const sourceDefaultRoleCap = 80
	c := EventConfiguration{ThreadLocale: "ru", AmountCapPerRole: sourceDefaultRoleCap}
	var err error
	c.ShortTitles, err = eventTitles(record["title_short"], id)
	if err != nil {
		return c, err
	}
	for key, target := range map[string]*string{"country_emoji": &c.CountryEmoji, "thread_locale": &c.ThreadLocale} {
		if raw, ok := record[key]; ok {
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, target) != nil ||
				!eventConfigurationText(*target) {
				return c, errors.New("event_configuration_invalid")
			}
		}
	}
	if err = c.readNumbers(record); err != nil {
		return c, err
	}
	if raw, ok := record["thread_channel"]; ok {
		c.ThreadChannel, err = eventChannel(raw)
	}
	return c, err
}

func (c *EventConfiguration) readNumbers(record map[string]json.RawMessage) error {
	if raw, ok := record["amount_cap_per_role"]; ok &&
		(bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &c.AmountCapPerRole) != nil) {
		return errors.New("event_configuration_invalid")
	}
	if raw, ok := record["price"]; ok && json.Unmarshal(raw, &c.DefaultPrice) != nil {
		return errors.New("event_configuration_invalid")
	}
	if raw, ok := record["thread_id"]; ok && json.Unmarshal(raw, &c.ThreadID) != nil {
		return errors.New("event_configuration_invalid")
	}
	return nil
}

func eventConfigurationText(value string) bool {
	const maxConfigurationText = 1024
	return len(value) <= maxConfigurationText && !strings.ContainsRune(value, 0)
}

func eventChannel(raw json.RawMessage) (string, error) {
	if bytes.Equal(raw, []byte("null")) {
		return "", errors.New("event_channel_invalid")
	}
	var channel string
	if json.Unmarshal(raw, &channel) == nil {
		if !eventConfigurationText(channel) {
			return "", errors.New("event_channel_invalid")
		}
		if channel == "" {
			return "", nil
		}
		return "@" + channel, nil
	}
	var channelID int64
	if json.Unmarshal(raw, &channelID) != nil {
		return "", errors.New("event_channel_invalid")
	}
	return strconv.FormatInt(channelID, 10), nil
}

func eventMaximumInstant() time.Time {
	value, _ := time.Parse(time.RFC3339Nano, "9999-12-31T23:59:59.999999Z")
	return value
}
