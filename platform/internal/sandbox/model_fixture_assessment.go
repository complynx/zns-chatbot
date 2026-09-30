package sandbox

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const maxFixtureAssessmentBody = 16 * 1024
const maxFixtureAssessmentID = 100

type fixtureAssessmentInput struct {
	Event   *string `json:"event"`
	Topic   *string `json:"topic"`
	FactKey *string `json:"fact_key"`
	Text    *string `json:"text"`
}

type fixtureAssessmentResult struct {
	Worthwhile *bool   `json:"worthwhile"`
	Reason     *string `json:"reason"`
}

type frozenFixtureAssessment struct {
	expect agent.KnowledgeAssessmentInput
	result agent.KnowledgeAssessment
}

type modelFixtureAssessmentState struct {
	Configured bool   `json:"configured"`
	Accepted   int    `json:"accepted"`
	Rejected   int    `json:"rejected"`
	LastStatus string `json:"last_status"`
}

// Assessment fields use exact protocol names, unlike encoding/json's folded
// struct-field matching. Existing plan-envelope fields retain their behavior.
func (v *modelFixtureInstall) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := strictFixtureJSON(data, &fields); err != nil {
		return err
	}
	for key := range fields {
		if strings.EqualFold(key, "assessment") && key != "assessment" {
			return errors.New("invalid assessment field")
		}
	}
	type wire modelFixtureInstall
	return strictFixtureJSON(data, (*wire)(v))
}

func exactAssessmentJSON(data []byte, target any, keys ...string) error {
	var fields map[string]json.RawMessage
	if err := strictFixtureJSON(data, &fields); err != nil {
		return err
	}
	if len(fields) != len(keys) {
		return errors.New("invalid assessment fields")
	}
	for _, key := range keys {
		if _, exists := fields[key]; !exists {
			return errors.New("invalid assessment fields")
		}
	}
	return strictFixtureJSON(data, target)
}

func (v *fixtureAssessmentInput) UnmarshalJSON(data []byte) error {
	type wire fixtureAssessmentInput
	return exactAssessmentJSON(data, (*wire)(v), "event", "topic", "fact_key", "text")
}

func (v *fixtureAssessmentResult) UnmarshalJSON(data []byte) error {
	type wire fixtureAssessmentResult
	return exactAssessmentJSON(data, (*wire)(v), "worthwhile", "reason")
}

func fixtureAssessmentText(value *string, limit int) bool {
	return value != nil && utf8.ValidString(*value) && utf8.RuneCountInString(*value) <= limit &&
		!strings.ContainsRune(*value, 0)
}

func (v fixtureAssessmentInput) input() (agent.KnowledgeAssessmentInput, error) {
	if !fixtureAssessmentText(v.Event, maxFixtureAssessmentID) ||
		!fixtureAssessmentText(v.Topic, maxFixtureAssessmentID) ||
		!fixtureAssessmentText(v.FactKey, maxFixtureAssessmentID) ||
		!fixtureAssessmentText(v.Text, knowledge.MaxText) ||
		strings.TrimSpace(*v.Text) == "" {
		return agent.KnowledgeAssessmentInput{}, errors.New("invalid assessment input")
	}
	return agent.KnowledgeAssessmentInput{Event: *v.Event, Topic: *v.Topic, FactKey: *v.FactKey, Text: *v.Text}, nil
}

func freezeFixtureAssessment(data json.RawMessage) (*frozenFixtureAssessment, error) {
	var wire struct {
		Expect fixtureAssessmentInput  `json:"expect"`
		Result fixtureAssessmentResult `json:"result"`
	}
	if len(data) > maxModelFixtureStepBytes || !utf8.Valid(data) ||
		exactAssessmentJSON(data, &wire, "expect", "result") != nil {
		return nil, errors.New("invalid fixture assessment")
	}
	input, err := wire.Expect.input()
	if err != nil || wire.Result.Worthwhile == nil ||
		!fixtureAssessmentText(wire.Result.Reason, knowledge.MaxMemoText) {
		return nil, errors.New("invalid fixture assessment")
	}
	result := agent.KnowledgeAssessment{Worthwhile: *wire.Result.Worthwhile, Reason: *wire.Result.Reason}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 4096 {
		return nil, errors.New("fixture assessment exceeds budget")
	}
	return &frozenFixtureAssessment{expect: input, result: result}, nil
}

func freezeModelFixture(value modelFixtureInstall) ([]frozenModelStep, *frozenFixtureAssessment, int, error) {
	var assessment *frozenFixtureAssessment
	var err error
	if len(value.Assessment) != 0 {
		assessment, err = freezeFixtureAssessment(value.Assessment)
		if err != nil {
			return nil, nil, 0, err
		}
	}
	if len(value.Steps) == 0 && assessment != nil {
		return nil, assessment, len(value.Assessment), nil
	}
	steps, size, err := freezeModelSteps(value.Steps)
	return steps, assessment, size + len(value.Assessment), err
}

func (m *modelFixtures) assess(
	scope modelFixtureScope,
	input agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.cases[modelFixtureKey(scope.Owner, scope.UpdateID)]
	if !ok {
		return agent.KnowledgeAssessment{}, errors.New("fixture not configured")
	}
	status := fixtureAccepted
	switch {
	case value.assessment == nil:
		status = "not_configured"
	case value.assessment.expect.Event != input.Event || value.assessment.expect.Topic != input.Topic ||
		value.assessment.expect.FactKey != input.FactKey || value.assessment.expect.Text != input.Text:
		status = "input_mismatch"
	case value.assessmentState.Accepted != 0:
		status = "already_consumed"
	}
	value.assessmentState.LastStatus = status
	if status != fixtureAccepted {
		value.assessmentState.Rejected++
		return agent.KnowledgeAssessment{}, errors.New("fixture assessment " + status)
	}
	value.assessmentState.Accepted++
	return value.assessment.result, nil
}

func (f *Fake) modelFixtureAssessment(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	update, err := strconv.ParseInt(r.Header.Get("X-Sandbox-Update"), 10, 64)
	owner := r.Header.Get("X-Sandbox-Actor")
	if err != nil || update <= 0 || !syntheticFixtureOwner(owner) {
		api.JSON(w, http.StatusForbidden, map[string]string{errorField: invalidFixtureScope})
		return
	}
	var wire fixtureAssessmentInput
	if err = readFixtureBody(w, r, maxFixtureAssessmentBody, &wire); err != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{errorField: "invalid assessment input"})
		return
	}
	input, err := wire.input()
	if err != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{errorField: "invalid assessment input"})
		return
	}
	verdict, err := f.modelFixtures.assess(modelFixtureScope{Owner: owner, UpdateID: update}, input)
	if err != nil {
		api.JSON(w, http.StatusConflict, map[string]string{errorField: err.Error()})
		return
	}
	api.JSON(w, http.StatusOK, verdict)
}
