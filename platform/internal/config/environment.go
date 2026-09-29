package config

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func isDuration(field reflect.Type) bool { return field == reflect.TypeFor[time.Duration]() }

// Keep aliases explicit: an unrecognized unprefixed process variable is unrelated.
func aliases() map[string]string {
	pairs := []struct{ legacy, canonical string }{
		{"DATABASE_URL", "ZNS_DATABASE__URL"},
		{"LINEUP_CSV", "ZNS_LINEUP__CSV"},
		{"LINEUP_EVENT_YEAR", "ZNS_LINEUP__EVENT_YEAR"},
		{"LINEUP_TIMEZONE", "ZNS_LINEUP__TIMEZONE"},
		{"SANDBOX_SIGNING_KEY", "ZNS_AUTH__SIGNING_KEY"},
		{"BIND_HOST", "ZNS_SERVER__HOST"},
		{"PORT", "ZNS_SERVER__PORT"},
		{"CORE_URL", "ZNS_CORE__URL"},
		{"TELEGRAM_BASE_URL", "ZNS_TELEGRAM__BASE_URL"},
		{"TELEGRAM_TOKEN", "ZNS_TELEGRAM__TOKEN"},
		{"WEBAPP_URL", "ZNS_TELEGRAM__WEB_APP_URL"},
		{"MINIAPP_URL", "ZNS_SANDBOX__MINI_APP_URL"},
		{"MODEL_PROVIDER", "ZNS_MODEL__PROVIDER"},
		{"MODEL_URL", "ZNS_MODEL__URL"},
		{"OPENAI_API_KEY", "ZNS_MODEL__OPENAI_KEY"},
		{"CODEX_EXECUTABLE", "ZNS_MODEL__CODEX_EXECUTABLE"},
		{"SYNTHETIC_ONLY", "ZNS_SYNTHETIC_ONLY"},
		{"MEDIA_WORKER_URL", "ZNS_MEDIA__URL"},
		{"MEDIA_WORKER_SECRET", "ZNS_MEDIA__SECRET"},
		{"STICKER_WORKER_URL", "ZNS_STICKER__WORKER__URL"},
		{"STICKER_WORKER_SECRET", "ZNS_STICKER__WORKER__SECRET"},
		{"STICKER_CACHE_CAPACITY", "ZNS_STICKER__CACHE__CAPACITY"},
		{"STICKER_CACHE_HALF_LIFE", "ZNS_STICKER__CACHE__HALF_LIFE"},
		{"ORDER_REMINDER_AFTER", "ZNS_ORDERS__REMINDER_AFTER"},
	}
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		result[pair.legacy] = pair.canonical
	}
	return result
}

func applyEnvironment(config *Config, environ []string) error {
	fields := map[string]reflect.Value{}
	fieldsOf(reflect.ValueOf(config).Elem(), "", fields)
	legacy := aliases()
	values, collectErr := environmentValues(environ, fields, legacy)
	if collectErr != nil {
		return collectErr
	}
	for alias, canonical := range legacy {
		value, present := values[alias]
		_, overridden := values[canonical]
		// Historical env helpers treat an empty legacy variable as absent.
		if present && value != "" && !overridden {
			if err := setField(fields[canonical], value, canonical); err != nil {
				return err
			}
		}
	}
	for name, field := range fields {
		if value, present := values[name]; present {
			if err := setField(field, value, name); err != nil {
				return err
			}
		}
	}
	return nil
}

func environmentValues(
	environ []string,
	fields map[string]reflect.Value,
	legacy map[string]string,
) (map[string]string, error) {
	values := map[string]string{}
	for _, entry := range environ {
		name, value, found := strings.Cut(entry, "=")
		// The command adapter consumes the optional YAML filename before Load.
		if name == "ZNS_CONFIG_FILE" {
			continue
		}
		_, alias := legacy[name]
		if !strings.HasPrefix(name, "ZNS_") && !alias {
			continue
		}
		if !found || len(value) > maxConfigBytes {
			return nil, errors.New("invalid configuration environment entry")
		}
		if _, known := fields[name]; !known && !alias {
			return nil, fmt.Errorf("unknown configuration environment variable %s", name)
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("duplicate configuration environment variable %s", name)
		}
		values[name] = value
	}
	return values, nil
}

func setField(field reflect.Value, value, name string) error {
	invalid := fmt.Errorf("configuration environment variable %s has an invalid value", name)
	if isDuration(field.Type()) {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return invalid
		}
		field.SetInt(int64(duration))
		return nil
	}
	switch {
	case field.Kind() == reflect.String:
		field.SetString(value)
	case field.Kind() == reflect.Bool:
		if value != trueValue && value != "false" {
			return invalid
		}
		field.SetBool(value == trueValue)
	case field.Kind() == reflect.Int:
		number, err := strconv.ParseInt(value, 10, field.Type().Bits())
		if err != nil {
			return invalid
		}
		field.SetInt(number)
	case field.Kind() == reflect.Float64:
		number, err := strconv.ParseFloat(value, field.Type().Bits())
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return invalid
		}
		field.SetFloat(number)
	default:
		return invalid
	}
	return nil
}
