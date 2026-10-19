// Package events defines event configuration and its time-zone contract.
package events

import (
	"errors"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // The release image does not provide system IANA zone files.

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

const MaxModules = 100
const MaxModuleIDLength = 128
const MaxNameLength = 256

var ErrInvalid = errors.New("invalid event configuration")

type Document struct {
	Deposit      *registrations.DepositPolicy `json:"deposit,omitempty"`
	Registration *registrations.Rules         `json:"registration,omitempty"`
	Name         string                       `json:"name"                   minLength:"1" maxLength:"256"`
	Timezone     string                       `json:"timezone"               minLength:"1" maxLength:"128"`
	StartsAt     time.Time                    `json:"starts_at"`
	EndsAt       time.Time                    `json:"ends_at"`
	Modules      []string                     `json:"modules"                                              maxItems:"100" uniqueItems:"true"`
}

type Event struct {
	ID       uuid.UUID `json:"id"`
	Version  int64     `json:"version"`
	Document Document  `json:"document"`
}

// Normalize validates absolute instants and a named zone; it never guesses a local UTC offset.
func Normalize(document Document) (Document, error) {
	document.Name = strings.TrimSpace(document.Name)
	if document.Name == "" || len([]rune(document.Name)) > MaxNameLength ||
		document.Timezone == "" || document.Timezone == "Local" || len(document.Timezone) > MaxModuleIDLength ||
		!validWindow(document.StartsAt, document.EndsAt) || len(document.Modules) > MaxModules {
		return Document{}, ErrInvalid
	}
	if _, err := time.LoadLocation(document.Timezone); err != nil {
		return Document{}, ErrInvalid
	}
	document.StartsAt, document.EndsAt = document.StartsAt.UTC(), document.EndsAt.UTC()
	document.Modules = append([]string{}, document.Modules...)
	sort.Strings(document.Modules)
	for i, module := range document.Modules {
		if !validModule(module) || (i > 0 && module == document.Modules[i-1]) {
			return Document{}, ErrInvalid
		}
	}
	if document.Registration != nil {
		rules, err := document.Registration.Normalize()
		if err != nil || rules.ClosesAt.After(document.EndsAt) {
			return Document{}, ErrInvalid
		}
		document.Registration = &rules
	}
	if document.Deposit != nil {
		if document.Registration == nil {
			return Document{}, ErrInvalid
		}
		policy := *document.Deposit
		if err := policy.ValidatePrices(*document.Registration); err != nil {
			return Document{}, ErrInvalid
		}
		document.Deposit = &policy
	}
	return document, nil
}

func validWindow(start, end time.Time) bool {
	return !start.IsZero() && end.After(start) &&
		start.Nanosecond()%int(time.Microsecond) == 0 && end.Nanosecond()%int(time.Microsecond) == 0
}

func validModule(id string) bool {
	if id == "" || len(id) > MaxModuleIDLength || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, ch := range id {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' && ch != '.' {
			return false
		}
	}
	return true
}

// LocalTimes returns event instants in their display zone, including historical DST rules.
func (e Event) LocalTimes() (time.Time, time.Time, error) {
	zone, err := time.LoadLocation(e.Document.Timezone)
	if err != nil || e.Document.Timezone == "Local" {
		return time.Time{}, time.Time{}, ErrInvalid
	}
	return e.Document.StartsAt.In(zone), e.Document.EndsAt.In(zone), nil
}
