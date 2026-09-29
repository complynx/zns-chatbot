package sandbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

const maxModelFixtureBytes = 256 * 1024
const maxModelFixtureStepBytes = 64 * 1024
const maxModelFixtureSteps = 32
const maxModelFixtureCases = 32
const maxModelFixtureInputBytes = 512 * 1024

// Expectations are partial Input objects: maps match subsets, arrays match exactly.
// Plans remain proposals; normal bot/API authorization must still execute them.
type modelFixtureStep struct {
	Expect json.RawMessage `json:"expect"`
	Plan   agent.Plan      `json:"plan"`
}

type modelFixtureInstall struct {
	Input    *modelFixtureInput `json:"input,omitempty"`
	Owner    string             `json:"owner"`
	UpdateID int64              `json:"update_id"`
	Steps    []modelFixtureStep `json:"steps"`
}

type modelFixtureInput struct {
	User         int64  `json:"user"`
	Text         string `json:"text"`
	LanguageCode string `json:"language_code"`
}

type modelFixtureScope struct {
	Owner    string
	UpdateID int64
	Turn     int
}

type frozenModelStep struct {
	expect any
	plan   []byte
}

type modelFixtureCase struct {
	steps      []frozenModelStep
	next       int
	accepted   int
	rejected   int
	lastStatus string
}

type modelFixtures struct {
	mu    sync.Mutex
	cases map[string]*modelFixtureCase
	bytes int
	steps int
}

type modelFixtureState struct {
	UpdateID   int64  `json:"update_id"`
	NextTurn   int    `json:"next_turn"`
	Total      int    `json:"total"`
	Accepted   int    `json:"accepted"`
	Rejected   int    `json:"rejected"`
	LastStatus string `json:"last_status"`
}

func modelFixtureKey(owner string, update int64) string {
	return owner + ":" + strconv.FormatInt(update, 10)
}

func syntheticFixtureOwner(owner string) bool {
	return owner == "alice" || owner == "bob" || owner == "visitor"
}

func freezeModelSteps(steps []modelFixtureStep) ([]frozenModelStep, int, error) {
	if len(steps) == 0 || len(steps) > maxModelFixtureSteps {
		return nil, 0, errors.New("invalid fixture step count")
	}
	frozen := make([]frozenModelStep, 0, len(steps))
	size := 0
	for _, step := range steps {
		encoded, err := json.Marshal(step)
		if err != nil || len(encoded) > maxModelFixtureStepBytes {
			return nil, 0, errors.New("fixture step exceeds budget")
		}
		if err = agent.Validate(step.Plan); err != nil {
			return nil, 0, errors.New("invalid fixture plan")
		}
		var input agent.Input
		if err = strictFixtureJSON(step.Expect, &input); err != nil {
			return nil, 0, errors.New("invalid expected input")
		}
		var expected map[string]any
		decoder := json.NewDecoder(bytes.NewReader(step.Expect))
		decoder.UseNumber()
		if err = decoder.Decode(&expected); err != nil {
			return nil, 0, errors.New("invalid expected input")
		}
		if _, ok := expected["text"].(string); !ok {
			return nil, 0, errors.New("expected text is required")
		}
		plan, _ := json.Marshal(step.Plan)
		frozen = append(frozen, frozenModelStep{expect: expected, plan: plan})
		size += len(encoded)
	}
	return frozen, size, nil
}

func (m *modelFixtures) install(value modelFixtureInstall) error {
	if !syntheticFixtureOwner(value.Owner) || value.UpdateID <= 0 {
		return errors.New("invalid fixture scope")
	}
	steps, size, err := freezeModelSteps(value.Steps)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cases == nil {
		m.cases = map[string]*modelFixtureCase{}
	}
	key := modelFixtureKey(value.Owner, value.UpdateID)
	if _, exists := m.cases[key]; exists {
		return errors.New("fixture already configured")
	}
	if len(m.cases) >= maxModelFixtureCases || m.steps+len(steps) > maxModelFixtureSteps ||
		m.bytes+size > maxModelFixtureBytes {
		return errors.New("fixture capacity reached")
	}
	m.cases[key] = &modelFixtureCase{steps: steps, lastStatus: "ready"}
	m.steps += len(steps)
	m.bytes += size
	return nil
}

func (m *modelFixtures) plan(scope modelFixtureScope, input agent.Input) (agent.Plan, error) {
	if !syntheticFixtureOwner(scope.Owner) || scope.UpdateID <= 0 || scope.Turn < 0 {
		return agent.Plan{}, errors.New("invalid fixture scope")
	}
	data, err := json.Marshal(input)
	if err != nil || len(data) > maxModelFixtureInputBytes {
		return agent.Plan{}, errors.New("fixture input exceeds budget")
	}
	var actual any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = decoder.Decode(&actual); err != nil {
		return agent.Plan{}, errors.New("invalid fixture input")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.cases[modelFixtureKey(scope.Owner, scope.UpdateID)]
	if !ok {
		return agent.Plan{}, errors.New("fixture not configured")
	}
	if scope.Turn != value.next || value.next >= len(value.steps) {
		value.rejected++
		value.lastStatus = "sequence_mismatch"
		return agent.Plan{}, errors.New("fixture sequence mismatch")
	}
	step := value.steps[value.next]
	if !fixtureSubset(step.expect, actual) {
		value.rejected++
		value.lastStatus = "input_mismatch"
		return agent.Plan{}, errors.New("fixture input mismatch")
	}
	var plan agent.Plan
	if err = json.Unmarshal(step.plan, &plan); err != nil {
		return agent.Plan{}, errors.New("invalid stored fixture")
	}
	value.next++
	value.accepted++
	value.lastStatus = "accepted"
	return plan, nil
}

func fixtureSubset(expected, actual any) bool {
	switch value := expected.(type) {
	case map[string]any:
		object, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, want := range value {
			got, present := object[key]
			if !present || !fixtureSubset(want, got) {
				return false
			}
		}
		return true
	case []any:
		array, ok := actual.([]any)
		if !ok || len(array) != len(value) {
			return false
		}
		for index, want := range value {
			if !fixtureSubset(want, array[index]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(expected, actual)
	}
}
