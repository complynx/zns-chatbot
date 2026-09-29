package config

import (
	"errors"
	"time"
)

type Lineup struct {
	CSV       string `yaml:"csv"        json:"csv"`
	EventYear int    `yaml:"event_year" json:"event_year"`
	Timezone  string `yaml:"timezone"   json:"timezone"`
}

func (value Lineup) validate() error {
	if value.CSV == "" && value.EventYear == 0 && value.Timezone == "" {
		return nil
	}
	if value.CSV == "" || value.EventYear < 1 || value.EventYear > 9998 || value.Timezone == "" ||
		value.Timezone == "Local" {
		return errors.New("configuration lineup requires csv, event_year (1..9998), and explicit timezone")
	}
	if _, err := time.LoadLocation(value.Timezone); err != nil {
		return errors.New("configuration lineup.timezone is invalid")
	}
	return nil
}
