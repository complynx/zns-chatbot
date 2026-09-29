package migrate

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

const massageMaximumLength = 1000
const massageSpecialistField = "massage_specialist"
const massagePrintNameField = "print_name"

type MassageSpecialist struct {
	Owner    int64             `json:"owner"`
	Name     string            `json:"name"`
	Icon     string            `json:"icon"`
	About    map[string]string `json:"about"`
	Min      int               `json:"min"`
	Max      int               `json:"max"`
	Table    bool              `json:"table"`
	Bookings bool              `json:"bookings"`
	Next     bool              `json:"next"`
	Work     []MassageParty    `json:"work"`
}

func convertMassageSpecialist(user map[string]json.RawMessage) (*MassageSpecialist, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(user[massageSpecialistField], &fields) != nil || fields == nil {
		return nil, errors.New("massage_specialist_shape_invalid")
	}
	var allowed strings.Builder
	allowed.WriteString(
		"name icon about min_duration max_duration table_not_required notify_bookings notify_next work_hours",
	)
	for key := range fields {
		if strings.HasPrefix(key, "about_") && len(key) > len("about_") {
			allowed.WriteString(" " + key)
		}
	}
	f, err := objectFields(user[massageSpecialistField], allowed.String())
	if err != nil {
		return nil, errors.New("massage_specialist_field_unmapped")
	}
	p := &MassageSpecialist{
		Icon:     "💆‍♂️",
		Min:      1,
		Max:      massageMaximumLength,
		Bookings: true,
		Next:     true,
		About:    map[string]string{},
	}
	var ok bool
	p.Owner, ok = telegramNumber(user["user_id"])
	if !ok {
		return nil, errors.New("massage_specialist_owner_invalid")
	}
	if err = p.readDescription(f, user); err != nil {
		return nil, err
	}
	if err = p.readPreferences(f); err != nil {
		return nil, err
	}
	if err = p.readWork(f); err != nil {
		return nil, err
	}
	return p, nil
}
func massageFallbackName(user map[string]json.RawMessage, id int64) string {
	name := ""
	for _, key := range []string{"inner_name_en", massagePrintNameField} {
		if raw, present := user[key]; present {
			if json.Unmarshal(raw, &name) == nil {
				break
			}
		}
	}
	if _, a := user["inner_name_en"]; !a {
		if _, b := user[massagePrintNameField]; !b {
			var first, last string
			_ = json.Unmarshal(user["first_name"], &first)
			_ = json.Unmarshal(user["last_name"], &last)
			name = strings.TrimSpace(first + " " + last)
		}
	}
	if name == "" {
		_ = json.Unmarshal(user["username"], &name)
	}
	if name == "" {
		name = strconv.FormatInt(id, 10)
	}
	return name
}

func (p *MassageSpecialist) readDescription(f, user map[string]json.RawMessage) error {
	var err error
	if _, exists := f["name"]; exists {
		p.Name, err = orderString(f, "name", false)
	} else {
		p.Name = massageFallbackName(user, p.Owner)
	}
	if err != nil {
		return err
	}
	if _, exists := f["icon"]; exists {
		p.Icon, err = orderString(f, "icon", false)
		if err != nil {
			return err
		}
	}
	about, err := orderString(f, "about", true)
	if err != nil {
		return errors.New("massage_about_unresolved")
	}
	for _, locale := range []string{"en", "ru"} {
		p.About[locale] = about
		if _, exists := f["about_"+locale]; exists {
			p.About[locale], err = orderString(f, "about_"+locale, false)
			if err != nil {
				return err
			}
		}
	}
	for key := range f {
		if strings.HasPrefix(key, "about_") {
			value, e := orderString(f, key, false)
			if e != nil {
				return e
			}
			p.About[strings.TrimPrefix(key, "about_")] = value
		}
	}

	return nil
}

func (p *MassageSpecialist) readPreferences(f map[string]json.RawMessage) error {
	var err error
	for key, target := range map[string]*int{"min_duration": &p.Min, "max_duration": &p.Max} {
		if raw, exists := f[key]; exists {
			if json.Unmarshal(raw, target) != nil {
				return errors.New("massage_duration_invalid")
			}
		}
	}
	if p.Min < 1 || p.Min > 6 || p.Max < p.Min || p.Max > 1000 {
		return errors.New("massage_duration_invalid")
	}
	for key, target := range map[string]*bool{"notify_bookings": &p.Bookings, "notify_next": &p.Next, "table_not_required": &p.Table} {
		if err = foodOptionalBool(f, key, target); err != nil {
			return err
		}
	}

	return nil
}

func (p *MassageSpecialist) readWork(f map[string]json.RawMessage) error {
	var rows []json.RawMessage
	if json.Unmarshal(f["work_hours"], &rows) != nil || rows == nil {
		return errors.New("massage_work_unresolved")
	}
	p.Work = []MassageParty{}
	for _, row := range rows {
		fields, e := objectFields(row, "start end")
		if e != nil {
			return errors.New("massage_work_invalid")
		}
		span := MassageParty{}
		span.Start, e = eventInstant(fields["start"])
		if e != nil {
			return e
		}
		span.End, e = eventInstant(fields["end"])
		if e != nil {
			return e
		}
		if !span.End.After(span.Start) || span.End.Sub(span.Start) > 48*time.Hour {
			return errors.New("massage_work_invalid")
		}
		p.Work = append(p.Work, span)
	}

	return nil
}
