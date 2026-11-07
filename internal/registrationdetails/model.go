package registrationdetails

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Field string

const (
	LegalName          Field = "legal_name"
	PassportNumber     Field = "passport_number"
	maxLegalNameLength       = 256
	maxPassportLength        = 128
)

var ErrInvalid = errors.New("invalid registration details")

// Record is private legal data, never a general profile or agent-context value.
type Record struct {
	LegalName      string `json:"legal_name"`
	PassportNumber string `json:"passport_number"`
}

// Status is safe to persist in receipts and automatic agent context.
type Status struct {
	Version               int64 `json:"version"`
	LegalNamePresent      bool  `json:"legal_name_present"`
	PassportNumberPresent bool  `json:"passport_number_present"`
}

type Mutation struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Field           Field     `json:"field"`
	Value           string    `json:"value"`
}

func (m Mutation) Normalize() (Mutation, error) {
	if m.OperationID == uuid.Nil || m.ExpectedVersion < 0 || !utf8.ValidString(m.Value) {
		return Mutation{}, ErrInvalid
	}
	m.Value = strings.TrimSpace(m.Value)
	limit := maxLegalNameLength
	switch m.Field {
	case LegalName:
	case PassportNumber:
		limit = maxPassportLength
	default:
		return Mutation{}, ErrInvalid
	}
	if m.Value == "" || utf8.RuneCountInString(m.Value) > limit {
		return Mutation{}, ErrInvalid
	}
	for _, r := range m.Value {
		if unicode.IsControl(r) {
			return Mutation{}, ErrInvalid
		}
	}
	return m, nil
}

func (r Record) Apply(m Mutation) (Record, error) {
	normalized, err := m.Normalize()
	if err != nil {
		return Record{}, err
	}
	if normalized.Field == LegalName {
		r.LegalName = normalized.Value
	} else {
		r.PassportNumber = normalized.Value
	}
	return r, nil
}

func (r Record) Status(version int64) Status {
	return Status{Version: version, LegalNamePresent: r.LegalName != "", PassportNumberPresent: r.PassportNumber != ""}
}
