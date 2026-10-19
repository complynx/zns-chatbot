package registrationdetails

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Requirements belong to an event; the saved owner record remains independent.
type Requirements struct {
	LegalName      bool `json:"legal_name"      required:"false"`
	PassportNumber bool `json:"passport_number" required:"false"`
}

// UnmarshalJSON defaults omitted flags to false and rejects nulls/unknown fields.
func (r *Requirements) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ErrInvalid
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	type document Requirements
	var decoded document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil {
		return ErrInvalid
	}
	*r = Requirements(decoded).Normalize()
	return nil
}

func (r Requirements) Normalize() Requirements {
	if r.PassportNumber {
		r.LegalName = true
	}
	return r
}

func (r Requirements) Missing(status Status) []Field {
	r = r.Normalize()
	missing := []Field{}
	if r.LegalName && !status.LegalNamePresent {
		missing = append(missing, LegalName)
	}
	if r.PassportNumber && !status.PassportNumberPresent {
		missing = append(missing, PassportNumber)
	}
	return missing
}

var ErrRequired = errors.New("required legal registration details missing")

// MissingError contains field identifiers and a version, never private values.
type MissingError struct {
	MissingFields  []Field
	DetailsVersion int64
}

func (*MissingError) Error() string { return ErrRequired.Error() }
func (*MissingError) Unwrap() error { return ErrRequired }
