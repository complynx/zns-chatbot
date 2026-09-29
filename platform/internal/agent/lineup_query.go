package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/lineup"
)

const MaxLineupReads = 4
const planLineupField = "lineup_action"

// LineupQuery filters public event evidence; it carries no file path or identity.
type LineupQuery struct {
	Scope  string `json:"scope"`
	Date   string `json:"date"`
	Room   string `json:"room"`
	DJ     string `json:"dj"`
	Cursor string `json:"cursor"`
}

func (query LineupQuery) Validate() error {
	if query.Scope != lineupCurrent && query.Scope != lineupDay && query.Scope != lineupFull {
		return errors.New("invalid lineup scope")
	}
	const maxFilterRunes = 128
	if len(query.Cursor) > maxLineupCursorBytes || !boundedKnowledgeText(query.Room, maxFilterRunes) ||
		!boundedKnowledgeText(query.DJ, maxFilterRunes) {
		return errors.New("invalid lineup query")
	}
	if query.Date != "" {
		if _, err := time.Parse(time.DateOnly, query.Date); err != nil {
			return errors.New("invalid lineup event date")
		}
	}
	return nil
}

// Query can reach any matching page; the cursor counts matches, not CSV rows.
func (source *LineupSource) Query(now time.Time, query LineupQuery) (LineupRead, error) {
	if err := query.Validate(); err != nil {
		return LineupRead{}, err
	}
	if source == nil || source.schedule == nil {
		return LineupRead{Scope: query.Scope, Query: query, Status: lineupUnavailable}, nil
	}
	position, err := source.lineupPosition(query, now)
	if err != nil {
		return LineupRead{}, err
	}
	now = position.Now
	var entries []lineup.Entry
	empty := "no_schedule"
	switch query.Scope {
	case lineupCurrent:
		entries, empty = source.schedule.Current(now), "no_current_party"
	case lineupDay:
		entries, empty = source.schedule.Day(now), "no_party_today"
	case lineupFull:
		for _, group := range source.schedule.Full() {
			entries = append(entries, group.Entries...)
		}
	}
	if query.Date != "" || query.Room != "" || query.DJ != "" || query.Cursor != "" {
		empty = "no_matches"
	}
	read := lineupRead(query, position.Offset, empty, entries)
	read.AsOf = now.In(source.location)
	if read.Omitted {
		position.Offset += len(read.Entries)
		read.NextCursor = source.lineupCursor(position)
	}
	return read, nil
}

func validateLineupPlan(plan Plan) error {
	if plan.LineupAction == nil {
		return nil
	}
	if plan.View != workflowView || plan.Action != nil || plan.OrderAction != nil || plan.ProfileAction != nil ||
		plan.MediaAction != nil || plan.KnowledgeAction != nil || plan.ScriptAction != nil ||
		plan.HistoryAction != nil || plan.RegistrationAction != nil {
		return errors.New("conflicting lineup query")
	}
	return plan.LineupAction.Validate()
}

func validateContextPlans(plan Plan) error {
	for _, validate := range []func(Plan) error{
		validateLineupPlan, validateRegistrationPlan, validateHistoryPlan, validateScriptPlan,
	} {
		if err := validate(plan); err != nil {
			return err
		}
	}
	return nil
}

func codexLineupFields(data []byte) error {
	fields, err := codexObject(data)
	if err != nil || len(fields) != 5 {
		return errors.New("invalid lineup query fields")
	}
	for _, field := range []string{"scope", "date", "room", "dj", "cursor"} {
		if len(fields[field]) == 0 || bytes.Equal(fields[field], []byte("null")) {
			return errors.New("missing lineup query field")
		}
	}
	var query LineupQuery
	if json.Unmarshal(data, &query) != nil {
		return errors.New("invalid lineup query types")
	}
	return query.Validate()
}
