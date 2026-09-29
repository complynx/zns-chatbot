package migrate

import (
	"encoding/json"
	"errors"
	"strconv"
)

type massageDraftState struct {
	Party    string                        `json:"party"`
	Length   int                           `json:"length"`
	Page     int                           `json:"page"`
	Selected map[string]bool               `json:"selected"`
	Choices  map[string]massageDraftChoice `json:"choices"`
}
type massageDraftChoice struct {
	Party      string `json:"party,omitempty"`
	Page       *int   `json:"page,omitempty"`
	Slot       *int   `json:"slot,omitempty"`
	Specialist string `json:"specialist,omitempty"`
}

func normalizeMassageDraft(
	raw []byte,
	b *MassageCandidate,
	p preparedMassage,
	owners map[int64]string,
) (massageDraftState, error) {
	state := massageDraftState{Selected: map[string]bool{}, Choices: map[string]massageDraftChoice{}}
	var f map[string]json.RawMessage
	if json.Unmarshal(raw, &f) != nil {
		return state, errors.New("massage_draft_invalid")
	}
	if v, ok := f["length"]; ok {
		if json.Unmarshal(v, &state.Length) != nil || state.Length < 1 || state.Length > 6 {
			return state, errors.New("massage_draft_length_invalid")
		}
	}
	if v, ok := f["page"]; ok {
		if json.Unmarshal(v, &state.Page) != nil {
			return state, errors.New("massage_draft_page_invalid")
		}
	}
	var err error
	if v, ok := f["party"]; ok {
		state.Party, err = draftParty(v, b.Event, p)
		if err != nil {
			return state, err
		}
	}
	if err = state.readSelected(f, p, owners); err != nil {
		return state, err
	}
	if err = state.readChoices(f, b.Event, p, owners); err != nil {
		return state, err
	}
	return state, nil
}
func draftParty(raw json.RawMessage, event string, p preparedMassage) (string, error) {
	var day int
	if json.Unmarshal(raw, &day) != nil {
		return "", errors.New("massage_draft_party_invalid")
	}
	for _, party := range p.Configurations[event].Parties {
		if day == party.day() {
			return massagePartyID(event, day), nil
		}
	}
	return "", errors.New("massage_draft_party_unresolved")
}

func normalizeMassageDraftChoice(
	raw []byte,
	event string,
	p preparedMassage,
	owners map[int64]string,
) (massageDraftChoice, error) {
	c := massageDraftChoice{}
	f, err := objectFields(raw, "party page slot specialist")
	if err != nil {
		return c, errors.New("massage_draft_choice_unmapped")
	}
	switch {
	case len(f) == 1 && f["party"] != nil:
		c.Party, err = draftParty(f["party"], event, p)
	case len(f) == 1 && f["page"] != nil:
		var delta int
		if json.Unmarshal(f["page"], &delta) != nil || delta < -1 || delta > 1 {
			return c, errors.New("massage_draft_page_invalid")
		}
		c.Page = &delta
	case f["specialist"] != nil && (len(f) == 1 || len(f) == 2 && f["slot"] != nil):
		id, ok := telegramNumber(f["specialist"])
		if !ok || p.Specialists[id] == nil || owners[id] == "" {
			return c, errors.New("massage_draft_specialist_unresolved")
		}
		c.Specialist = owners[id]
		if v, exists := f["slot"]; exists {
			var slot int
			if json.Unmarshal(v, &slot) != nil || slot < -144 || slot > 288 {
				return c, errors.New("massage_draft_slot_invalid")
			}
			c.Slot = &slot
		}
	default:
		return c, errors.New("massage_draft_choice_invalid")
	}
	return c, err
}

func (state *massageDraftState) readSelected(
	f map[string]json.RawMessage,
	p preparedMassage,
	owners map[int64]string,
) error {
	if v, ok := f["specialists_choices"]; ok {
		var selected map[string]json.RawMessage
		if json.Unmarshal(v, &selected) != nil || selected == nil {
			return errors.New("massage_draft_selected_invalid")
		}
		for key, value := range selected {
			id, e := strconv.ParseInt(key, 10, 64)
			if e != nil || strconv.FormatInt(id, 10) != key || p.Specialists[id] == nil || owners[id] == "" {
				return errors.New("massage_draft_specialist_unresolved")
			}
			var flag bool
			if e = foodOptionalBool(map[string]json.RawMessage{"value": value}, "value", &flag); e != nil {
				return e
			}
			state.Selected[owners[id]] = flag
		}
	}

	return nil
}

func (state *massageDraftState) readChoices(
	f map[string]json.RawMessage,
	event string,
	p preparedMassage,
	owners map[int64]string,
) error {
	if v, ok := f["choices"]; ok {
		var choices map[string]json.RawMessage
		if json.Unmarshal(v, &choices) != nil || choices == nil {
			return errors.New("massage_draft_choices_invalid")
		}
		for key, value := range choices {
			n, e := strconv.Atoi(key)
			if e != nil || n < 0 || strconv.Itoa(n) != key {
				return errors.New("massage_draft_choice_key_invalid")
			}
			choice, e := normalizeMassageDraftChoice(value, event, p, owners)
			if e != nil {
				return e
			}
			state.Choices[key] = choice
		}
	}

	return nil
}
