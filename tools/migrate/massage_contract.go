package migrate

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Bundle IANA rules for offline Windows imports.
)

const massageBufferMinutes = 5

const massageSource = "massage"

const massageFields = "_id bot_id user_id pass_key created_at length party slot start end specialist finalized_at deleted choices specialists_choices page error client_notified_prior_long client_notified specialist_notified notify"

// MassageCandidate preserves existence-based flags separately from their values in source evidence.
type MassageCandidate struct {
	ID             string     `json:"id"`
	Event          string     `json:"event"`
	Owner          int64      `json:"owner"`
	Specialist     int64      `json:"specialist"`
	Day            int        `json:"day"`
	Slot           int        `json:"slot"`
	Length         int        `json:"length"`
	Start          time.Time  `json:"start"`
	End            time.Time  `json:"end"`
	Created        *time.Time `json:"created,omitempty"`
	Finalized      *time.Time `json:"finalized,omitempty"`
	Deleted        bool       `json:"deleted"`
	Draft          bool       `json:"draft"`
	LongSent       bool       `json:"long_sent"`
	ShortSent      bool       `json:"short_sent"`
	SpecialistSent bool       `json:"specialist_sent"`
	Additional     bool       `json:"additional"`
}

func convertMassage(raw []byte, bot int64) (*MassageCandidate, error) {
	f, err := objectFields(raw, massageFields)
	if err != nil {
		return nil, errors.New("massage_field_unmapped")
	}
	p := &MassageCandidate{}
	id, err := orderTargetID(f["_id"], bot)
	if err != nil {
		return nil, err
	}
	p.ID = strings.Replace(id, "legacy-order:", "legacy-massage:", 1)
	var ok bool
	p.Owner, ok = telegramNumber(f["user_id"])
	if !ok {
		return nil, errors.New("massage_owner_invalid")
	}
	p.Event, err = orderString(f, "pass_key", true)
	if err != nil || !tokenPattern.MatchString(p.Event) {
		return nil, errors.New("massage_event_unresolved")
	}
	_, p.Deleted = f["deleted"]
	_, p.LongSent = f["client_notified_prior_long"]
	_, p.ShortSent = f["client_notified"]
	_, p.SpecialistSent = f["specialist_notified"]
	if err = foodOptionalBool(f, "notify", &p.Additional); err != nil {
		return nil, err
	}
	if err = foodOptionalTime(f["created_at"], &p.Created); err != nil {
		return nil, err
	}
	if err = foodOptionalTime(f["finalized_at"], &p.Finalized); err != nil {
		return nil, err
	}
	_, finished := f["start"]
	p.Draft = !finished
	if !finished {
		return p, nil
	}
	return p, p.readFinalized(f)
}

type MassageConfiguration struct {
	Event      string         `json:"event_key"`
	DailyLimit int            `json:"daily_limit"`
	PriorLong  int64          `json:"prior_long_seconds"`
	PriorShort int64          `json:"prior_short_seconds"`
	Parties    []MassageParty `json:"parties"`
}
type MassageParty struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Tables int       `json:"massage_tables"`
	Open   bool      `json:"is_open"`
}

func (p MassageParty) day() int {
	location, err := time.LoadLocation("Europe/Minsk")
	if err != nil {
		return 0
	}
	return p.Start.In(location).Day()
}
func massagePartyID(event string, day int) string {
	return "legacy-massage-party:" + event + ":" + strconv.Itoa(day)
}
func convertMassageConfiguration(raw []byte) (*MassageConfiguration, error) {
	f, err := objectFields(raw, "_id kind event_key daily_limit prior_long_seconds prior_short_seconds parties")
	if err != nil {
		return nil, errors.New("massage_configuration_unmapped")
	}
	c := &MassageConfiguration{}
	if json.Unmarshal(raw, c) != nil || !tokenPattern.MatchString(c.Event) || c.DailyLimit < 1 || c.DailyLimit > 100 ||
		c.PriorLong <= 0 ||
		c.PriorShort <= 0 ||
		len(c.Parties) == 0 {
		return nil, errors.New("massage_configuration_invalid")
	}
	var rows []json.RawMessage
	if json.Unmarshal(f["parties"], &rows) != nil {
		return nil, errors.New("massage_parties_invalid")
	}
	days := map[int]bool{}
	for i, row := range rows {
		fields, e := objectFields(row, "start end massage_tables is_open")
		if e != nil {
			return nil, errors.New("massage_party_unmapped")
		}
		p := &c.Parties[i]
		if len(fields["massage_tables"]) == 0 || string(fields["massage_tables"]) == "null" ||
			json.Unmarshal(fields["massage_tables"], &p.Tables) != nil ||
			len(fields["is_open"]) == 0 {
			return nil, errors.New("massage_party_policy_unresolved")
		}
		if e = foodOptionalBool(fields, "is_open", &p.Open); e != nil {
			return nil, e
		}
		p.Start, e = eventInstant(fields["start"])
		if e != nil {
			return nil, e
		}
		p.End, e = eventInstant(fields["end"])
		if e != nil {
			return nil, e
		}
		if p.Tables < 0 || p.Tables > 100 || !p.End.After(p.Start) || p.End.Sub(p.Start) > 48*time.Hour ||
			days[p.day()] ||
			p.day() < 1 {
			return nil, errors.New("massage_party_ambiguous")
		}
		days[p.day()] = true
	}
	return c, nil
}

func (p *MassageCandidate) readFinalized(f map[string]json.RawMessage) error {
	var ok bool
	var err error
	p.Specialist, ok = telegramNumber(f["specialist"])
	if !ok {
		return errors.New("massage_specialist_unresolved")
	}
	if json.Unmarshal(f["length"], &p.Length) != nil || p.Length < 1 || p.Length > 6 {
		return errors.New("massage_length_invalid")
	}
	if json.Unmarshal(f["party"], &p.Day) != nil || p.Day < 1 || p.Day > 31 {
		return errors.New("massage_party_invalid")
	}
	if json.Unmarshal(f["slot"], &p.Slot) != nil || p.Slot < -144 || p.Slot > 288 {
		return errors.New("massage_slot_invalid")
	}
	p.Start, err = eventInstant(f["start"])
	if err != nil {
		return err
	}
	p.End = p.Start.Add(time.Duration(p.Length*20-massageBufferMinutes) * time.Minute)
	if value, exists := f["end"]; exists {
		end, e := eventInstant(value)
		if e != nil || !end.Equal(p.End) {
			return errors.New("massage_end_conflict")
		}
	}
	if p.Finalized == nil {
		return errors.New("massage_finalized_time_unresolved")
	}
	return nil
}
