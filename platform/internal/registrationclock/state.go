// Package registrationclock defines the one reviewed synthetic case clock
// publication shared by the app reader and trusted operator.
package registrationclock

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const Bytes = 4096
const Horizon = 60 * time.Hour
const Installation = "010400000204"
const Case = "c-registration-clock-20261001-v1"
const Path = "/run/registration-clock/state.json"
const DatabaseAddress = "postgres:5432/synthetic_qa_zns_registration_fixture"
const Stand = "synthetic-qa-zns-registration-fixture"
const Database = "synthetic_qa_zns_registration_fixture"
const Marker = "registration-clock:" + Installation + ":" + Case

type Settings struct {
	File            string
	Installation    string
	Case            string
	DatabaseAddress string
	Anchor          string
}

func (c Settings) Enabled() bool {
	return c.File != "" || c.Installation != "" || c.Case != "" || c.DatabaseAddress != "" || c.Anchor != ""
}

func (c Settings) Validate() error {
	if c.File != Path || c.Installation != Installation || c.Case != Case {
		return errors.New("registration clock requires the exact private file, installation and case")
	}
	if c.DatabaseAddress != DatabaseAddress {
		return errors.New("registration clock requires the allocated database address")
	}
	_, err := c.AnchorTime()
	return err
}

func (c Settings) AnchorTime() (time.Time, error) {
	anchor, err := time.Parse(time.RFC3339Nano, c.Anchor)
	if err != nil || !strings.HasSuffix(c.Anchor, "Z") || anchor.IsZero() || anchor.Nanosecond()%1000 != 0 {
		return time.Time{}, errors.New("registration clock requires the immutable UTC microsecond setup anchor")
	}
	return anchor, nil
}

type State struct {
	Version         int       `json:"version"`
	Installation    string    `json:"installation"`
	Case            string    `json:"case"`
	Stand           string    `json:"stand"`
	DatabaseAddress string    `json:"database_address"`
	Anchor          time.Time `json:"anchor"`
	Current         time.Time `json:"current"`
	Revision        uint64    `json:"revision"`
	digest          [32]byte
}

// Digest identifies the exact bytes read, including same-revision rewrites.
func (s State) Digest() [32]byte { return s.digest }

func Decode(raw []byte) (State, error) {
	var state State
	if len(raw) > Bytes {
		return state, errors.New("registration clock state exceeds bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return state, fmt.Errorf("decode registration clock state: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return state, errors.New("registration clock state has trailing content")
	}
	state.digest = sha256.Sum256(raw)
	return state, nil
}

func (s State) Validate(settings Settings) error {
	anchor, err := settings.AnchorTime()
	if err != nil {
		return err
	}
	if s.Version != 1 || s.Revision == 0 || s.Stand != Stand || s.Installation != Installation || s.Case != Case ||
		s.DatabaseAddress != DatabaseAddress ||
		s.Installation != settings.Installation || s.Case != settings.Case ||
		s.DatabaseAddress != settings.DatabaseAddress {
		return errors.New("registration clock state allocation or case mismatch")
	}
	_, anchorOffset := s.Anchor.Zone()
	_, currentOffset := s.Current.Zone()
	if !s.Anchor.Equal(anchor) || s.Current.IsZero() || anchorOffset != 0 || currentOffset != 0 ||
		s.Anchor.Nanosecond()%1000 != 0 || s.Current.Nanosecond()%1000 != 0 ||
		s.Current.Before(s.Anchor) || s.Current.After(s.Anchor.Add(Horizon)) {
		return errors.New("registration clock state exceeds monotonic microsecond horizon")
	}
	return nil
}
