package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
)

type preparedFood struct {
	EventPlanSHA256 string
	Proofs          map[string]foodProof
	Plan            FoodPlan
	PlanHash        string
	ResolutionHash  string
	Events          []preparedFoodEvent
	Users           map[int64]OrderDependency
	Dependencies    map[string]OrderDependency
}

type preparedFoodEvent struct {
	Catalog FoodPlanRecord
	Menu    json.RawMessage
	Orders  []FoodPlanRecord
	Markers []FoodPassMarker
}

func prepareFood(stage, planPath, resolutionsPath string, limits Limits) (preparedFood, error) {
	p := preparedFood{Users: map[int64]OrderDependency{}, Dependencies: map[string]OrderDependency{}}
	plan, generated, err := preparedFoodPlan(stage, limits)
	if err != nil {
		return p, err
	}
	original, err := readApplyFile(planPath, maxUserPlanBytes)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(generated, original) {
		return p, errors.New("apply_plan_mismatch")
	}
	raw, err := readApplyFile(resolutionsPath, maxUserResolutionBytes)
	if err != nil {
		return p, err
	}
	if _, err = objectFields(
		raw,
		"version plan_sha256 dates_verified configuration_verified admin_grants_verified bot_namespace_verified writers_stopped",
	); err != nil {
		return p, errors.New("resolution_invalid")
	}
	var resolution OrderResolutions
	if json.Unmarshal(raw, &resolution) != nil || resolution.Version != 1 || !resolution.DatesVerified ||
		!resolution.ConfigurationVerified ||
		!resolution.AdminGrantsVerified ||
		!resolution.BotNamespaceVerified ||
		!resolution.WritersStopped {
		return p, errors.New("resolution_attestation_required")
	}
	p.Plan, p.PlanHash, p.ResolutionHash = plan, hashBytes(original), hashBytes(raw)
	if resolution.PlanSHA256 != p.PlanHash {
		return p, errors.New("resolution_plan_mismatch")
	}
	if err = p.group(); err != nil {
		return p, err
	}
	_, eventPlan, err := preparedEventPlan(stage, limits)
	if err != nil {
		return p, err
	}
	p.EventPlanSHA256 = hashBytes(eventPlan)
	root, _, err := verifiedUsersStage(stage, limits)
	if err != nil {
		return p, err
	}
	defer func() { _ = root.Close() }()
	if err = p.loadFoodMenus(root); err != nil {
		return p, err
	}
	if err = p.loadFoodProofs(root); err != nil {
		return p, err
	}
	return p, nil
}

func (p *preparedFood) group() error {
	if err := p.groupDependencies(); err != nil {
		return err
	}
	events := map[string]*preparedFoodEvent{}
	for _, row := range p.Plan.Records {
		if len(row.Blockers) > 0 {
			return errors.New("food_plan_blocked")
		}
		if row.Configuration == nil || row.Excluded {
			continue
		}
		key := row.Configuration.Event
		if events[key] != nil {
			return errors.New("food_configuration_ambiguous")
		}
		events[key] = &preparedFoodEvent{Catalog: row}
	}
	for _, row := range p.Plan.Records {
		if row.Food == nil || row.Excluded {
			continue
		}
		event := events[row.Food.Event]
		if event == nil {
			return errors.New("food_configuration_missing")
		}
		event.Orders = append(event.Orders, row)
	}
	for _, marker := range p.Plan.Markers {
		event := events[marker.Event]
		if event == nil {
			return errors.New("food_marker_configuration_missing")
		}
		event.Markers = append(event.Markers, marker)
	}
	keys := []string{}
	for key := range events {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		p.Events = append(p.Events, *events[key])
	}
	return nil
}

func (p *preparedFood) loadFoodMenus(root *os.Root) error {
	for i := range p.Events {
		c := p.Events[i].Catalog.Configuration
		var entry File
		for _, f := range p.Plan.Files {
			if f.Source == orderConfigurationSource && f.Path == c.MenuFile && f.Kind == foodResourceKind {
				entry = f
			}
		}
		if entry.Path == "" || entry.SHA256 != c.MenuSHA256 {
			return errors.New("food_menu_resource_unresolved")
		}
		file, openErr := openRegular(root, entry.Path)
		if openErr != nil {
			return openErr
		}
		body, readErr := io.ReadAll(io.LimitReader(file, entry.Bytes+1))
		_ = file.Close()
		if readErr != nil || int64(len(body)) != entry.Bytes || hashBytes(body) != entry.SHA256 ||
			validJSON(body) != nil {
			return errors.New("food_menu_resource_changed")
		}
		p.Events[i].Menu = body
		if err := validateFoodMenu(body, p.Events[i].Orders); err != nil {
			return err
		}
	}
	return nil
}

func (p *preparedFood) groupDependencies() error {
	for _, dep := range p.Plan.Dependencies {
		switch dep.Source {
		case usersSource:
			if _, exists := p.Users[dep.TelegramID]; exists {
				return errors.New("food_owner_ambiguous")
			}
			p.Users[dep.TelegramID] = dep
		case eventsSource:
			if _, exists := p.Dependencies[dep.EventID]; exists {
				return errors.New("food_event_ambiguous")
			}
			p.Dependencies[dep.EventID] = dep
		}
	}
	return nil
}
