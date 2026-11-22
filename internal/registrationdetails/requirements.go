package registrationdetails

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Requirements belong to an event; the saved owner record remains independent.
type Requirements struct {
	LegalName         bool     `json:"legal_name" required:"false"`
	PassportNumber    bool     `json:"passport_number" required:"false"`
	PassportCountries []string `json:"passport_countries,omitempty" maxItems:"249" required:"false"`
	PassportPhoto     bool     `json:"passport_photo,omitempty" required:"false"`
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
	return r.Validate()
}

func (r Requirements) Normalize() Requirements {
	r.PassportCountries = slices.Clone(r.PassportCountries)
	for i, country := range r.PassportCountries {
		r.PassportCountries[i] = strings.ToUpper(strings.TrimSpace(country))
	}
	if r.PassportNumber || r.PassportPhoto || len(r.PassportCountries) > 0 {
		r.LegalName = true
	}
	return r
}

func (r Requirements) Validate() error {
	seen := make(map[string]bool, len(r.PassportCountries))
	for _, country := range r.PassportCountries {
		if !validCountry(country) || seen[country] {
			return ErrInvalid
		}
		seen[country] = true
	}
	return nil
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
	if len(r.PassportCountries) > 0 && !status.PassportCountryPresent {
		missing = append(missing, PassportCountry)
	}
	if r.PassportPhoto && !status.PassportPhotoPresent {
		missing = append(missing, PassportPhoto)
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
