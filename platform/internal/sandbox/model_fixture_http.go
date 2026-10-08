package sandbox

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
)

func (f *Fake) modelFixtureRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /lab/model/fixtures", f.installModelFixture)
	mux.HandleFunc("GET /lab/model/state", f.modelFixtureState)
	mux.HandleFunc("POST /lab/model/plan", f.modelFixturePlan)
	mux.HandleFunc("POST /lab/model/knowledge-assessment", f.modelFixtureAssessment)
}

func (f *Fake) installModelFixture(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	var value modelFixtureInstall
	if err := readFixtureBody(w, r, maxModelFixtureBytes, &value); err != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{errorField: "invalid fixture"})
		return
	}
	if value.Hold != nil && (f.delay == nil || subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("X-R104-Control")), []byte(f.delay.key)) != 1) {
		api.JSON(w, http.StatusForbidden, map[string]string{errorField: "model control authorization required"})
		return
	}
	update, err := f.installAndEnqueueFixture(r.Context(), value)
	if err != nil {
		api.JSON(w, http.StatusConflict, map[string]string{errorField: err.Error()})
		return
	}
	api.JSON(w, http.StatusCreated, map[string]any{"installed": true, "update_id": update})
}

func (f *Fake) modelFixturePlan(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	update, updateErr := strconv.ParseInt(r.Header.Get("X-Sandbox-Update"), 10, 64)
	turn, turnErr := strconv.Atoi(r.Header.Get("X-Sandbox-Turn"))
	owner := r.Header.Get("X-Sandbox-Actor")
	if updateErr != nil || turnErr != nil || !attestedModelOwner(f.attestedOwners, owner) || update <= 0 || turn < 0 {
		api.JSON(w, http.StatusForbidden, map[string]string{errorField: invalidFixtureScope})
		return
	}
	var input agent.Input
	if err := readFixtureBody(w, r, maxModelFixtureInputBytes, &input); err != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{errorField: "invalid fixture input"})
		return
	}
	scope := modelFixtureScope{Owner: owner, UpdateID: update, Turn: turn}
	plan, err := f.fixtureModelPlan(r.Context(), scope, input)
	if err != nil {
		if errors.Is(err, http.ErrAbortHandler) {
			panic(http.ErrAbortHandler)
		}
		if errors.Is(err, errModelFixtureUnavailable) {
			api.JSON(w, http.StatusServiceUnavailable, map[string]string{errorField: "fixture provider unavailable"})
			return
		}
		api.JSON(w, http.StatusConflict, map[string]string{errorField: err.Error()})
		return
	}
	if f.modelControl.cancelled(r.Context(), scope, modelFixtureSelection{}) {
		api.JSON(w, http.StatusServiceUnavailable, map[string]string{errorField: "fixture provider unavailable"})
		return
	}
	api.JSON(w, http.StatusOK, plan)
}

func (f *Fake) modelFixtureState(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	owner := r.URL.Query().Get("owner")
	update, err := strconv.ParseInt(r.URL.Query().Get("update_id"), 10, 64)
	if !attestedModelOwner(f.attestedOwners, owner) || err != nil || update <= 0 {
		api.JSON(w, http.StatusBadRequest, map[string]string{errorField: invalidFixtureScope})
		return
	}
	f.modelFixtures.mu.Lock()
	value, ok := f.modelFixtures.cases[modelFixtureKey(owner, update)]
	var state modelFixtureState
	if ok {
		state = modelFixtureState{Assessment: value.assessmentState, UpdateID: update, NextTurn: value.next,
			Total: len(value.steps), Accepted: value.accepted, Rejected: value.rejected, LastStatus: value.lastStatus}
	}
	f.modelFixtures.mu.Unlock()
	if !ok {
		api.JSON(w, http.StatusNotFound, map[string]string{errorField: "fixture not configured"})
		return
	}
	api.JSON(
		w,
		http.StatusOK,
		state,
	)
}

func readFixtureBody(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return errors.New("fixture body exceeds budget")
	}
	return strictFixtureJSON(data, target)
}

func strictFixtureJSON(data []byte, target any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid fixture encoding")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	if err := fixtureJSONValue(keys, 0); err != nil {
		return err
	}
	if _, err := keys.Token(); err != io.EOF {
		return errors.New("trailing fixture input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// Reject ambiguity before typed decoding, including keys inside expected input.
func fixtureJSONValue(decoder *json.Decoder, depth int) error {
	const maxDepth = 32
	if depth > maxDepth {
		return errors.New("fixture nesting exceeds budget")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate fixture field")
			}
			seen[name] = true
		}
		if err = fixtureJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
