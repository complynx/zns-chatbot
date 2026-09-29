package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

type MassageResolutions struct {
	Version               int    `json:"version"`
	PlanSHA256            string `json:"plan_sha256"`
	DatesVerified         bool   `json:"dates_verified"`
	ConfigurationVerified bool   `json:"configuration_verified"`
	BotNamespaceVerified  bool   `json:"bot_namespace_verified"`
	WritersStopped        bool   `json:"writers_stopped"`
}
type preparedMassage struct {
	Plan           MassagePlan
	PlanHash       string
	ResolutionHash string
	Users          map[int64]OrderDependency
	Events         map[string]OrderDependency
	Configurations map[string]*MassageConfiguration
	Specialists    map[int64]*MassageSpecialist
}

func prepareMassage(stage, planPath, resolutionsPath string, limits Limits) (preparedMassage, error) {
	p := preparedMassage{
		Users:          map[int64]OrderDependency{},
		Events:         map[string]OrderDependency{},
		Configurations: map[string]*MassageConfiguration{},
		Specialists:    map[int64]*MassageSpecialist{},
	}
	plan, generated, err := preparedMassagePlan(stage, limits)
	if err != nil {
		return p, err
	}
	original, err := readApplyFile(planPath, maxUserPlanBytes)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(original, generated) {
		return p, errors.New("apply_plan_mismatch")
	}
	raw, err := readApplyFile(resolutionsPath, maxUserResolutionBytes)
	if err != nil {
		return p, err
	}
	if _, err = objectFields(
		raw,
		"version plan_sha256 dates_verified configuration_verified bot_namespace_verified writers_stopped",
	); err != nil {
		return p, errors.New("resolution_invalid")
	}
	p.Plan, p.PlanHash, p.ResolutionHash = plan, hashBytes(original), hashBytes(raw)
	var r MassageResolutions
	if json.Unmarshal(raw, &r) != nil || r.Version != 1 || r.PlanSHA256 != p.PlanHash || !r.DatesVerified ||
		!r.ConfigurationVerified ||
		!r.BotNamespaceVerified ||
		!r.WritersStopped {
		return p, errors.New("massage_resolution_required")
	}
	return p, p.index()
}
func (p *preparedMassage) eventNames() []string {
	names := []string{}
	for name := range p.Configurations {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
func (p *preparedMassage) validateBooking(b *MassageCandidate) error {
	c := p.Configurations[b.Event]
	if c == nil {
		return errors.New("massage_configuration_missing")
	}
	if _, ok := p.Users[b.Owner]; !ok {
		return errors.New("massage_owner_unresolved")
	}
	if b.Draft {
		return nil
	}
	if p.Specialists[b.Specialist] == nil {
		return errors.New("massage_specialist_unresolved")
	}
	for _, party := range c.Parties {
		if party.day() == b.Day {
			if !party.Start.Add(timeDurationSlots(b.Slot)).Equal(b.Start) {
				return errors.New("massage_party_start_conflict")
			}
			return nil
		}
	}
	return errors.New("massage_party_unresolved")
}

func (p *preparedMassage) dependencies() error {
	for _, d := range p.Plan.Dependencies {
		if d.Source == usersSource {
			if _, exists := p.Users[d.TelegramID]; exists {
				return errors.New("massage_owner_ambiguous")
			}
			p.Users[d.TelegramID] = d
		} else {
			if _, exists := p.Events[d.EventID]; exists {
				return errors.New("massage_event_ambiguous")
			}
			p.Events[d.EventID] = d
		}
	}
	return nil
}

func (p *preparedMassage) index() error {
	if err := p.dependencies(); err != nil {
		return err
	}
	for _, row := range p.Plan.Records {
		if err := p.indexRow(row); err != nil {
			return err
		}
	}
	if len(p.Configurations) == 0 {
		return errors.New("massage_configuration_required")
	}
	for _, row := range p.Plan.Records {
		if row.Excluded || row.Booking == nil {
			continue
		}
		if err := p.validateBooking(row.Booking); err != nil {
			return err
		}
	}
	return nil
}

func (p *preparedMassage) indexRow(row MassagePlanRecord) error {
	if len(row.Blockers) > 0 {
		return errors.New("massage_plan_blocked")
	}
	if row.Excluded {
		return nil
	}
	if c := row.Configuration; c != nil {
		if p.Configurations[c.Event] != nil {
			return errors.New("massage_configuration_ambiguous")
		}
		if p.Events[c.Event].EventID == "" {
			return errors.New("massage_event_unresolved")
		}
		p.Configurations[c.Event] = c
	}
	if s := row.Specialist; s != nil {
		if p.Specialists[s.Owner] != nil {
			return errors.New("massage_specialist_ambiguous")
		}
		p.Specialists[s.Owner] = s
	}
	return nil
}
