package agent

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/lineup"
)

const maxLineupReadBytes = 8 * 1024
const lineupCurrent = "current"
const lineupDay = "day"
const lineupFull = "full"
const lineupUnavailable = "unavailable"

// LineupSource holds an immutable startup snapshot. Restart to reload the CSV.
// A nil or zero source is unavailable, never a verified empty timetable.
type LineupSource struct {
	cursorKey string
	schedule  *lineup.Schedule
	location  *time.Location
	year      int
}

type LineupEntry struct {
	Truncated bool      `json:"truncated,omitempty"`
	Start     time.Time `json:"start"`
	DJ        string    `json:"dj"`
	Room      string    `json:"room"`
	EventDate string    `json:"event_date"`
}

type LineupRead struct {
	AsOf       time.Time     `json:"as_of"`
	Query      LineupQuery   `json:"query"`
	NextCursor string        `json:"next_cursor"`
	Scope      string        `json:"scope"`
	Status     string        `json:"status"`
	Entries    []LineupEntry `json:"entries,omitempty"`
	Omitted    bool          `json:"omitted"`
}

type LineupContext struct {
	Remaining int          `json:"remaining"`
	Now       time.Time    `json:"now"`
	Timezone  string       `json:"timezone,omitempty"`
	EventYear int          `json:"event_year,omitempty"`
	Reads     []LineupRead `json:"reads"`
}

// LoadLineup reads only an operator-configured local file. Errors contain no path
// or source text and must not be converted into a successful empty schedule.
func LoadLineup(path string, year int, timezone string) (*LineupSource, error) {
	location, err := time.LoadLocation(timezone)
	if timezone == "" || timezone == "Local" || err != nil {
		return nil, errors.New("lineup timezone unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("lineup file unavailable")
	}
	defer file.Close()
	schedule, err := lineup.Parse(file, year, location)
	if err != nil {
		return nil, errors.New("lineup CSV invalid")
	}
	return &LineupSource{schedule: schedule, location: location, year: year, cursorKey: rand.Text()}, nil
}

// Snapshot captures one host instant for all scopes. The host transports this
// bounded evidence internally; only selected scopes reach the model.
func (source *LineupSource) Snapshot(now time.Time) *LineupContext {
	result := &LineupContext{Now: now.UTC(), Remaining: MaxLineupReads}
	if source == nil || source.schedule == nil {
		for _, scope := range []string{lineupCurrent, lineupDay, lineupFull} {
			result.Reads = append(result.Reads, LineupRead{Scope: scope, Status: lineupUnavailable})
		}
		return result
	}
	result.Now = now.In(source.location)
	result.Timezone, result.EventYear = source.location.String(), source.year
	for _, scope := range []string{lineupCurrent, lineupDay, lineupFull} {
		read, _ := source.Query(now, LineupQuery{Scope: scope})
		result.Reads = append(result.Reads, read)
	}
	return result
}

func lineupRead(query LineupQuery, offset int, emptyStatus string, entries []lineup.Entry) LineupRead {
	result := LineupRead{Scope: query.Scope, Query: query, Status: emptyStatus}
	matched := 0
	for _, entry := range entries {
		date := entry.Start
		if date.Hour() < lineup.CutoffHour {
			date = date.AddDate(0, 0, -1)
		}
		if !query.matches(entry, date.Format(time.DateOnly)) {
			continue
		}
		matched++
		if matched <= offset {
			continue
		}
		result.Status = "available"
		item := LineupEntry{
			Start: entry.Start, DJ: entry.DJ, Room: entry.Room, EventDate: date.Format(time.DateOnly),
		}
		const maxLabelRunes = 256
		if len([]rune(item.DJ)) > maxLabelRunes || len([]rune(item.Room)) > maxLabelRunes {
			item.DJ = string([]rune(item.DJ)[:min(len([]rune(item.DJ)), maxLabelRunes)])
			item.Room = string([]rune(item.Room)[:min(len([]rune(item.Room)), maxLabelRunes)])
			item.Truncated = true
		}
		result.Entries = append(result.Entries, item)
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > maxLineupReadBytes-maxLineupCursorBytes {
			result.Entries = result.Entries[:len(result.Entries)-1]
			result.Omitted = true
			break
		}
	}
	return result
}

func (query LineupQuery) matches(entry lineup.Entry, date string) bool {
	return (query.Date == "" || query.Date == date) &&
		(query.Room == "" || strings.EqualFold(query.Room, entry.Room)) &&
		(query.DJ == "" || strings.Contains(strings.ToLower(entry.DJ), strings.ToLower(query.DJ)))
}

func selectLineupContext(input *Input, ids []skillID) {
	input.Lineup = nil
	for _, id := range ids {
		scope := lineupScope(id)
		if scope == "" {
			continue
		}
		if input.Lineup == nil {
			input.Lineup = &LineupContext{}
			if input.LineupSource != nil {
				input.Lineup.Remaining = input.LineupSource.Remaining
				input.Lineup.Now = input.LineupSource.Now
				input.Lineup.Timezone = input.LineupSource.Timezone
				input.Lineup.EventYear = input.LineupSource.EventYear
			}
		}
		read := LineupRead{Scope: scope, Status: lineupUnavailable}
		if input.LineupSource != nil {
			for _, candidate := range input.LineupSource.Reads {
				if candidate.Scope == scope {
					read = candidate
					break
				}
			}
		}
		input.Lineup.Reads = append(input.Lineup.Reads, read)
	}
}

func lineupScope(id skillID) string {
	switch id {
	case skillLineupCurrent:
		return lineupCurrent
	case skillLineupDay:
		return lineupDay
	case skillLineupFull:
		return lineupFull
	case skillBooking, skillOrders, skillProfile, skillReceipts, skillAV,
		skillStickers, skillKnowledge, skillScripting, skillHistory, skillRegistration:
		return ""
	default:
		return ""
	}
}
