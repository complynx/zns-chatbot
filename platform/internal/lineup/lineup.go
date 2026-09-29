// Package lineup reads the legacy DJ timetable and queries event-local times.
package lineup

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxBytes bounds both the CSV input and its in-memory representation.
	MaxBytes = 1 << 20
	// CutoffHour assigns early morning sets to the preceding event day.
	CutoffHour   = 7
	maxColumns   = 128
	maxRows      = 10000
	firstDataRow = 3
)

// Entry is a one-hour set in one room.
type Entry struct {
	Start time.Time
	DJ    string
	Room  string
}

// Group contains sets for an event date and room.
type Group struct {
	Date    time.Time
	Room    string
	Entries []Entry
}

// Schedule is immutable after parsing and safe for concurrent queries.
type Schedule struct {
	location *time.Location
	entries  []Entry
}

// Parse reads the source CSV with date headers, a room row, and time/DJ rows.
// Year and location must come from event configuration, never the host clock.
func Parse(input io.Reader, year int, location *time.Location) (*Schedule, error) {
	if input == nil || location == nil || year < 1 || year > 9998 {
		return nil, errors.New("lineup: input, event year (1..9998), and timezone are required")
	}

	data, err := io.ReadAll(io.LimitReader(input, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("lineup: read CSV: %w", err)
	}
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return nil, errors.New("lineup: CSV exceeds size limit or contains invalid UTF-8")
	}

	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("lineup: decode CSV: %w", err)
	}
	schedule := &Schedule{location: location}
	if len(rows) == 0 {
		return schedule, nil
	}
	if len(rows) < 2 || len(rows[0]) < 2 || len(rows[1]) < len(rows[0]) {
		return nil, errors.New("lineup: date headers and matching room row are required")
	}
	if len(rows) > maxRows || len(rows[0]) > maxColumns {
		return nil, errors.New("lineup: CSV exceeds row or column limit")
	}
	for column := 1; column < len(rows[0]); column++ {
		entries, columnErr := parseColumn(rows, column, year, location)
		if columnErr != nil {
			return nil, fmt.Errorf("lineup: column %d: %w", column+1, columnErr)
		}
		schedule.entries = append(schedule.entries, entries...)
	}
	slices.SortStableFunc(schedule.entries, func(a, b Entry) int { return a.Start.Compare(b.Start) })
	return schedule, nil
}

func parseColumn(rows [][]string, column, year int, location *time.Location) ([]Entry, error) {
	_, dateText, found := strings.Cut(rows[0][column], ",")
	if !found {
		return nil, errors.New("date header must contain a comma and DD.MM")
	}
	date, err := time.Parse("2006-2.1", fmt.Sprintf("%04d-%s", year, strings.TrimSpace(dateText)))
	if err != nil {
		return nil, fmt.Errorf("invalid date header: %w", err)
	}
	room := strings.TrimSpace(rows[1][column])
	if room == "" {
		return nil, errors.New("room name is required")
	}
	var entries []Entry
	for rowIndex, row := range rows[2:] {
		if column >= len(row) || strings.TrimSpace(row[column]) == "" {
			continue
		}
		clock, clockErr := time.Parse("15:4", strings.TrimSpace(row[0]))
		if clockErr != nil {
			continue // Legacy CSV ignores blank or invalid time rows.
		}
		day := date
		if clock.Hour() < CutoffHour {
			day = day.AddDate(0, 0, 1)
		}
		start, startErr := localStart(day, clock, location)
		if startErr != nil {
			return nil, fmt.Errorf("row %d: %w", rowIndex+firstDataRow, startErr)
		}
		entries = append(entries, Entry{Start: start, DJ: strings.TrimSpace(row[column]), Room: room})
	}
	return entries, nil
}

// localStart rejects wall times that CSV cannot identify uniquely across DST.
func localStart(day, clock time.Time, location *time.Location) (time.Time, error) {
	wall := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.UTC)
	start := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	if start.Format(time.DateTime) != wall.Format(time.DateTime) {
		return time.Time{}, errors.New("nonexistent local time")
	}
	_, offset := start.Zone()
	previous, next := start.ZoneBounds()
	if !previous.IsZero() {
		previous = previous.Add(-time.Second)
	}
	for _, boundary := range []time.Time{previous, next} {
		if boundary.IsZero() {
			continue
		}
		_, alternative := boundary.In(location).Zone()
		candidate := wall.Add(-time.Duration(alternative) * time.Second).In(location)
		if alternative != offset && candidate.Format(time.DateTime) == wall.Format(time.DateTime) {
			return time.Time{}, errors.New("ambiguous local time")
		}
	}
	return start, nil
}

// Current returns all sets in [start, start+1h), including simultaneous rooms.
func (schedule *Schedule) Current(now time.Time) []Entry {
	var entries []Entry
	for _, entry := range schedule.entries {
		if !now.Before(entry.Start) && now.Before(entry.Start.Add(time.Hour)) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// Day preserves the legacy daily query: the whole target calendar date plus
// the following morning before 07:00. Before 07:00 now selects the previous date.
func (schedule *Schedule) Day(now time.Time) []Entry {
	now = now.In(schedule.location)
	if now.Hour() < CutoffHour {
		now = now.AddDate(0, 0, -1)
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, schedule.location)
	end := time.Date(now.Year(), now.Month(), now.Day()+1, CutoffHour, 0, 0, 0, schedule.location)
	var entries []Entry
	for _, entry := range schedule.entries {
		if !entry.Start.Before(start) && entry.Start.Before(end) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// Full groups every set by event date and room, ordered by date then room.
func (schedule *Schedule) Full() []Group {
	var groups []Group
	indices := make(map[string]int)
	for _, entry := range schedule.entries {
		date := entry.Start
		if date.Hour() < CutoffHour {
			date = date.AddDate(0, 0, -1)
		}
		date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, schedule.location)
		key := date.Format(time.DateOnly) + "\x00" + entry.Room
		index, exists := indices[key]
		if !exists {
			index = len(groups)
			indices[key] = index
			groups = append(groups, Group{Date: date, Room: entry.Room})
		}
		groups[index].Entries = append(groups[index].Entries, entry)
	}
	slices.SortFunc(groups, func(a, b Group) int {
		if comparison := a.Date.Compare(b.Date); comparison != 0 {
			return comparison
		}
		return strings.Compare(a.Room, b.Room)
	})
	return groups
}
