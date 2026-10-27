package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

const ContextChoiceLimit = 20
const MaxContextRunIDBytes = 128

var ErrContextUnavailable = errors.New("event context unavailable")

// ContextEvent provides bounded participant-facing event metadata.
type ContextEvent struct {
	Deposit      *registrations.DepositPolicy `json:"deposit,omitempty"`
	ID           uuid.UUID                    `json:"id"`
	Version      int64                        `json:"version"`
	Name         string                       `json:"name"`
	Timezone     string                       `json:"timezone"`
	StartsAt     time.Time                    `json:"starts_at"`
	EndsAt       time.Time                    `json:"ends_at"`
	Registration *registrations.Rules         `json:"registration,omitempty"`
}

func ContextView(event Event) ContextEvent {
	return ContextEvent{
		Deposit:      event.Document.Deposit,
		ID:           event.ID,
		Version:      event.Version,
		Name:         event.Document.Name,
		Timezone:     event.Document.Timezone,
		StartsAt:     event.Document.StartsAt,
		EndsAt:       event.Document.EndsAt,
		Registration: event.Document.Registration,
	}
}

// Context suggests an event; callers must recheck module and account access at execution.
type Context struct {
	Source           string         `json:"source"`
	Selected         *ContextEvent  `json:"selected,omitempty"`
	Choices          []ContextEvent `json:"choices"`
	More             bool           `json:"more"`
	SelectionVersion int64          `json:"selection_version"`
}

type ContextList struct {
	Events []ContextEvent `json:"events"`
	Next   *uuid.UUID     `json:"next,omitempty"`
}

type ContextRequest struct {
	Module          string     `json:"module" minLength:"1" maxLength:"128"`
	EventID         *uuid.UUID `json:"event_id,omitempty"`
	HintID          *uuid.UUID `json:"hint_id,omitempty"`
	RequireExplicit bool       `json:"require_explicit,omitempty"`
}

func (r ContextRequest) Valid() bool {
	return validModule(r.Module) && (r.EventID == nil || *r.EventID != uuid.Nil) &&
		(r.HintID == nil || *r.HintID != uuid.Nil)
}
func contextID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// UnmarshalJSON rejects explicit null/zero targets instead of silently using another event.
func (r *ContextRequest) UnmarshalJSON(data []byte) error {
	type request ContextRequest
	var value request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ErrInvalid
	}
	for _, key := range []string{"event_id", "hint_id"} {
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return ErrInvalid
		}
	}
	result := ContextRequest(value)
	if !result.Valid() {
		return ErrInvalid
	}
	*r = result
	return nil
}

// ResolveContext chooses among already qualified events at one absolute instant.
// Ongoing events have equal priority; equal nearest start times also require clarification.
func ResolveContext(request ContextRequest, pinned uuid.UUID, candidates []Event, now time.Time) (Context, error) {
	if !request.Valid() {
		return Context{}, ErrInvalid
	}
	target, found, err := contextTarget(request, pinned, candidates)
	if err != nil || found {
		return target, err
	}
	if request.RequireExplicit {
		return boundedContext("clarify", contextViews(candidates)), nil
	}
	matches, source := automaticContext(candidates, now)
	switch len(matches) {
	case 0:
		return Context{Source: "none", Choices: []ContextEvent{}}, nil
	case 1:
		return Context{Source: source, Selected: &matches[0], Choices: []ContextEvent{}}, nil
	default:
		return boundedContext("clarify", matches), nil
	}
}
func contextTarget(request ContextRequest, pinned uuid.UUID, candidates []Event) (Context, bool, error) {
	for _, choice := range []struct {
		id     uuid.UUID
		source string
	}{{contextID(request.EventID), "explicit"}, {pinned, "pinned"}, {contextID(request.HintID), "hint"}} {
		if choice.id == uuid.Nil || (request.RequireExplicit && choice.source == "hint") {
			continue
		}
		for _, candidate := range candidates {
			if candidate.ID == choice.id {
				view := ContextView(candidate)
				return Context{Source: choice.source, Selected: &view, Choices: []ContextEvent{}}, true, nil
			}
		}
		if choice.source == "explicit" {
			return Context{}, false, ErrContextUnavailable
		}
	}
	return Context{}, false, nil
}
func contextViews(candidates []Event) []ContextEvent {
	views := make([]ContextEvent, 0, len(candidates))
	for _, candidate := range candidates {
		views = append(views, ContextView(candidate))
	}
	return views
}
func boundedContext(source string, choices []ContextEvent) Context {
	more := len(choices) > ContextChoiceLimit
	if more {
		choices = choices[:ContextChoiceLimit]
	}
	return Context{Source: source, Choices: choices, More: more}
}
func automaticContext(candidates []Event, now time.Time) ([]ContextEvent, string) {
	candidates = append([]Event(nil), candidates...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Document.StartsAt.Equal(candidates[j].Document.StartsAt) {
			return candidates[i].ID.String() < candidates[j].ID.String()
		}
		return candidates[i].Document.StartsAt.Before(candidates[j].Document.StartsAt)
	})
	current := []ContextEvent{}
	for _, candidate := range candidates {
		if !candidate.Document.StartsAt.After(now) && candidate.Document.EndsAt.After(now) {
			current = append(current, ContextView(candidate))
		}
	}
	if len(current) > 0 {
		return current, "current"
	}
	nearest := []ContextEvent{}
	for _, candidate := range candidates {
		if !candidate.Document.StartsAt.After(now) {
			continue
		}
		if len(nearest) > 0 && !candidate.Document.StartsAt.Equal(nearest[0].StartsAt) {
			break
		}
		nearest = append(nearest, ContextView(candidate))
	}
	return nearest, "nearest"
}
