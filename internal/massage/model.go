// Package massage models event-bound work schedules, not reservations or payments.
package massage

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxDocumentBytes = 48 << 10
	MaxLabelRunes    = 128
	MaxServices      = 16
	MaxWindows       = 32
	MaxSpecialists   = 32
	MaxWorkSpans     = 64
	MaxTotalSpans    = 512
	MaxBlocks        = 6
	MaxTables        = 64
	MinBlockSeconds  = 60
	MaxBlockSeconds  = 3600
	MaxCurrencyScale = 4
	MaxPrice         = int64(9_000_000_000_000_000)
	MaxWindowLength  = 48 * time.Hour
)

var ErrInvalid = errors.New("invalid massage schedule or candidate request")
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

type ScheduleLabel struct {
	EN string `json:"en" minLength:"1" maxLength:"128"`
	RU string `json:"ru" minLength:"1" maxLength:"128"`
}

type ScheduleService struct {
	ID         string        `json:"id"          pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name       ScheduleLabel `json:"name"`
	Blocks     int           `json:"blocks"                                        minimum:"1" maximum:"6"`
	PriceMinor int64         `json:"price_minor"                                   minimum:"0" maximum:"9000000000000000"`
}

type ScheduleWindow struct {
	ID       string        `json:"id"        pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name     ScheduleLabel `json:"name"`
	StartsAt time.Time     `json:"starts_at"`
	EndsAt   time.Time     `json:"ends_at"`
	Tables   int           `json:"tables"                                      minimum:"0" maximum:"64"`
}

type ScheduleWorkSpan struct {
	WindowID string    `json:"window_id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type ScheduleSpecialist struct {
	ID            string             `json:"id"             pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name          ScheduleLabel      `json:"name"`
	RequiresTable bool               `json:"requires_table"`
	ServiceIDs    []string           `json:"service_ids"                                      minItems:"1" maxItems:"16" uniqueItems:"true"`
	WorkSpans     []ScheduleWorkSpan `json:"work_spans"                                       minItems:"1" maxItems:"64"`
}

type ScheduleDocument struct {
	Currency      string               `json:"currency"       pattern:"^[A-Z]{3}$"`
	CurrencyScale int                  `json:"currency_scale"                      minimum:"0"  maximum:"4"`
	BlockSeconds  int                  `json:"block_seconds"                       minimum:"60" maximum:"3600"`
	BufferSeconds int                  `json:"buffer_seconds"                      minimum:"0"  maximum:"3599"`
	Services      []ScheduleService    `json:"services"                                                        minItems:"1" maxItems:"16"`
	Windows       []ScheduleWindow     `json:"windows"                                                         minItems:"1" maxItems:"32"`
	Specialists   []ScheduleSpecialist `json:"specialists"                                                     minItems:"1" maxItems:"32"`
}

// Required zero/false values must be explicit on the wire, never inferred from omissions.
func decodeRequired(data []byte, target any, fields ...string) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return ErrInvalid
	}
	for _, field := range fields {
		value, present := values[field]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (d *ScheduleDocument) UnmarshalJSON(data []byte) error {
	if len(data) > MaxDocumentBytes {
		return ErrInvalid
	}
	type document ScheduleDocument
	var decoded document
	if err := decodeRequired(
		data,
		&decoded,
		"currency",
		"currency_scale",
		"block_seconds",
		"buffer_seconds",
		"services",
		"windows",
		"specialists",
	); err != nil {
		return err
	}
	*d = ScheduleDocument(decoded)
	return nil
}

func (s *ScheduleSpecialist) UnmarshalJSON(data []byte) error {
	type specialist ScheduleSpecialist
	var decoded specialist
	if err := decodeRequired(data, &decoded, "id", "name", "requires_table", "service_ids", "work_spans"); err != nil {
		return err
	}
	*s = ScheduleSpecialist(decoded)
	return nil
}

func (s *ScheduleService) UnmarshalJSON(data []byte) error {
	type service ScheduleService
	var decoded service
	if err := decodeRequired(data, &decoded, "id", "name", "blocks", "price_minor"); err != nil {
		return err
	}
	*s = ScheduleService(decoded)
	return nil
}

func (w *ScheduleWindow) UnmarshalJSON(data []byte) error {
	type window ScheduleWindow
	var decoded window
	if err := decodeRequired(data, &decoded, "id", "name", "starts_at", "ends_at", "tables"); err != nil {
		return err
	}
	*w = ScheduleWindow(decoded)
	return nil
}

func normalizeLabel(label ScheduleLabel) (ScheduleLabel, error) {
	label.EN, label.RU = strings.TrimSpace(label.EN), strings.TrimSpace(label.RU)
	for _, value := range []string{label.EN, label.RU} {
		if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxLabelRunes {
			return ScheduleLabel{}, ErrInvalid
		}
		for _, char := range value {
			if unicode.IsControl(char) {
				return ScheduleLabel{}, ErrInvalid
			}
		}
	}
	return label, nil
}

func validInstant(value time.Time) bool {
	year := value.UTC().Year()
	return !value.IsZero() && year >= 1 && year <= 9999 && value.Nanosecond()%int(time.Microsecond) == 0
}

func validSpan(start, end time.Time) bool {
	return validInstant(start) && validInstant(end) && end.After(start)
}

// Normalize clones caller-owned slices and gives semantically identical schedules
// stable ordering for exact administrative approval hashes.
func Normalize(d ScheduleDocument) (ScheduleDocument, error) {
	if !currencyCode.MatchString(d.Currency) || d.CurrencyScale < 0 || d.CurrencyScale > MaxCurrencyScale ||
		d.BlockSeconds < MinBlockSeconds || d.BlockSeconds > MaxBlockSeconds || d.BufferSeconds < 0 || d.BufferSeconds >= d.BlockSeconds ||
		len(d.Services) < 1 || len(d.Services) > MaxServices || len(d.Windows) < 1 || len(d.Windows) > MaxWindows ||
		len(d.Specialists) < 1 || len(d.Specialists) > MaxSpecialists {
		return ScheduleDocument{}, ErrInvalid
	}
	d.Specialists = slices.Clone(d.Specialists)
	var err error
	var services map[string]bool
	d.Services, services, err = normalizeServices(d.Services)
	if err != nil {
		return ScheduleDocument{}, err
	}
	var windows map[string]ScheduleWindow
	d.Windows, windows, err = normalizeWindows(d.Windows)
	if err != nil {
		return ScheduleDocument{}, err
	}
	specialists := make(map[string]bool, len(d.Specialists))
	totalSpans := 0
	for index, specialist := range d.Specialists {
		if !identifier.MatchString(specialist.ID) || specialists[specialist.ID] {
			return ScheduleDocument{}, ErrInvalid
		}
		normalized, normalizeErr := normalizeSpecialist(specialist, services, windows)
		if normalizeErr != nil {
			return ScheduleDocument{}, normalizeErr
		}
		totalSpans += len(normalized.WorkSpans)
		if totalSpans > MaxTotalSpans {
			return ScheduleDocument{}, ErrInvalid
		}
		d.Specialists[index] = normalized
		specialists[specialist.ID] = true
	}
	sort.Slice(d.Services, func(i, j int) bool { return d.Services[i].ID < d.Services[j].ID })
	sort.Slice(d.Windows, func(i, j int) bool { return d.Windows[i].ID < d.Windows[j].ID })
	sort.Slice(d.Specialists, func(i, j int) bool { return d.Specialists[i].ID < d.Specialists[j].ID })
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > MaxDocumentBytes {
		return ScheduleDocument{}, ErrInvalid
	}
	return d, nil
}

func normalizeServices(values []ScheduleService) ([]ScheduleService, map[string]bool, error) {
	values = slices.Clone(values)
	services := make(map[string]bool, len(values))
	for index, service := range values {
		if !identifier.MatchString(service.ID) || services[service.ID] || service.Blocks < 1 ||
			service.Blocks > MaxBlocks ||
			service.PriceMinor < 0 ||
			service.PriceMinor > MaxPrice {
			return nil, nil, ErrInvalid
		}
		name, err := normalizeLabel(service.Name)
		if err != nil {
			return nil, nil, err
		}
		values[index].Name = name
		services[service.ID] = true
	}
	return values, services, nil
}

func normalizeWindows(values []ScheduleWindow) ([]ScheduleWindow, map[string]ScheduleWindow, error) {
	values = slices.Clone(values)
	windows := make(map[string]ScheduleWindow, len(values))
	for index, window := range values {
		if !identifier.MatchString(window.ID) || !validSpan(window.StartsAt, window.EndsAt) ||
			window.EndsAt.Sub(window.StartsAt) > MaxWindowLength ||
			window.Tables < 0 ||
			window.Tables > MaxTables {
			return nil, nil, ErrInvalid
		}
		if _, duplicate := windows[window.ID]; duplicate {
			return nil, nil, ErrInvalid
		}
		name, err := normalizeLabel(window.Name)
		if err != nil {
			return nil, nil, err
		}
		window.Name, window.StartsAt, window.EndsAt = name, window.StartsAt.UTC(), window.EndsAt.UTC()
		values[index] = window
		windows[window.ID] = window
	}
	return values, windows, nil
}

func normalizeSpecialist(
	s ScheduleSpecialist,
	services map[string]bool,
	windows map[string]ScheduleWindow,
) (ScheduleSpecialist, error) {
	if len(s.ServiceIDs) < 1 || len(s.ServiceIDs) > MaxServices || len(s.WorkSpans) < 1 ||
		len(s.WorkSpans) > MaxWorkSpans {
		return s, ErrInvalid
	}
	var err error
	s.Name, err = normalizeLabel(s.Name)
	if err != nil {
		return s, err
	}
	s.ServiceIDs = slices.Clone(s.ServiceIDs)
	sort.Strings(s.ServiceIDs)
	for index, service := range s.ServiceIDs {
		if !services[service] || (index > 0 && s.ServiceIDs[index-1] == service) {
			return s, ErrInvalid
		}
	}
	s.WorkSpans = slices.Clone(s.WorkSpans)
	for index, span := range s.WorkSpans {
		window, found := windows[span.WindowID]
		if !found || !validSpan(span.StartsAt, span.EndsAt) || span.StartsAt.Before(window.StartsAt) ||
			span.EndsAt.After(window.EndsAt) {
			return s, ErrInvalid
		}
		span.StartsAt, span.EndsAt = span.StartsAt.UTC(), span.EndsAt.UTC()
		s.WorkSpans[index] = span
	}
	sort.Slice(s.WorkSpans, func(i, j int) bool {
		if s.WorkSpans[i].WindowID != s.WorkSpans[j].WindowID {
			return s.WorkSpans[i].WindowID < s.WorkSpans[j].WindowID
		}
		return s.WorkSpans[i].StartsAt.Before(s.WorkSpans[j].StartsAt)
	})
	for index := 1; index < len(s.WorkSpans); index++ {
		previous, current := s.WorkSpans[index-1], s.WorkSpans[index]
		if previous.WindowID == current.WindowID && previous.EndsAt.After(current.StartsAt) {
			return s, ErrInvalid
		}
	}
	return s, nil
}

// FitsEvent checks aggregate bounds without extending them by legacy grace periods.
func FitsEvent(d ScheduleDocument, start, end time.Time) bool {
	if !validSpan(start, end) || len(d.Windows) == 0 {
		return false
	}
	for _, window := range d.Windows {
		if !validSpan(window.StartsAt, window.EndsAt) || window.StartsAt.Before(start) || window.EndsAt.After(end) {
			return false
		}
	}
	return true
}
