package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const modelHoldBefore = "before_consume"
const modelHoldAfter = "after_consume"
const modelHoldTimeout = 10 * time.Second

type modelFixtureHoldSetup struct {
	Turn int    `json:"turn"`
	Mode string `json:"mode"`
}

type modelFixtureHold struct {
	setup   modelFixtureHoldSetup
	release chan string
	claimed bool
	view    modelFixtureControlState
}

type modelFixtureSelection struct {
	mode    string
	release <-chan string
}

type modelFixtureControlState struct {
	Owner          string `json:"owner"`
	UpdateID       int64  `json:"update_id"`
	Turn           int    `json:"turn"`
	Mode           string `json:"mode,omitempty"`
	Phase          string `json:"phase"`
	Consumed       bool   `json:"consumed"`
	Durable        bool   `json:"durable"`
	RequestSHA256  string `json:"request_sha256,omitempty"`
	ResponseSHA256 string `json:"response_sha256,omitempty"`
}

// Selected model controls reuse the existing reserved authenticated listener.
// The control mutex never owns provider SQL, an HTTP wait or a file operation.
type modelFixtureControl struct {
	mu       sync.Mutex
	lifetime context.Context
	entries  map[string]*modelFixtureHold
}

func newModelFixtureControl(ctx context.Context) *modelFixtureControl {
	return &modelFixtureControl{lifetime: ctx, entries: map[string]*modelFixtureHold{}}
}

func stringTurn(turn int) string { return strconv.Itoa(turn) }

func (c *modelFixtureControl) validateInstall(value modelFixtureInstall) error {
	if value.Hold == nil {
		return nil
	}
	if value.Hold.Turn < 0 || value.Hold.Turn >= len(value.Steps) ||
		(value.Hold.Mode != modelHoldBefore && value.Hold.Mode != modelHoldAfter) {
		return errors.New("invalid model fixture hold")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxModelFixtureSteps {
		return errors.New("fixture control capacity reached")
	}
	return nil
}

func (c *modelFixtureControl) install(value modelFixtureInstall) {
	if value.Hold == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	scope := modelFixtureScope{Owner: value.Owner, UpdateID: value.UpdateID, Turn: value.Hold.Turn}
	c.entries[modelScopeKey(scope)] = &modelFixtureHold{
		setup:   *value.Hold,
		release: make(chan string, 1),
		view: modelFixtureControlState{
			Owner:    scope.Owner,
			UpdateID: scope.UpdateID,
			Turn:     scope.Turn,
			Mode:     value.Hold.Mode,
			Phase:    "armed",
		},
	}
}

func (c *modelFixtureControl) invalidateInstall(value modelFixtureInstall) {
	if value.Hold != nil {
		c.finish(
			modelFixtureScope{Owner: value.Owner, UpdateID: value.UpdateID, Turn: value.Hold.Turn},
			"setup_unavailable",
		)
	}
}

func (c *modelFixtureControl) restore(row modelConsumption) {
	c.consumed(row, true)
	c.finish(row.scope(), "consumed_unavailable")
}

func (c *modelFixtureControl) consumed(row modelConsumption, durable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := modelScopeKey(row.scope())
	hold := c.entries[key]
	if hold == nil {
		hold = &modelFixtureHold{}
		c.entries[key] = hold
	}
	hold.view = modelFixtureControlState{Owner: row.Owner, UpdateID: row.UpdateID, Turn: row.Turn,
		Mode: hold.setup.Mode, Phase: "consumed", Consumed: true, Durable: durable,
		RequestSHA256: row.RequestSHA256, ResponseSHA256: row.ResponseSHA256}
	if hold.claimed && hold.setup.Mode == modelHoldAfter {
		hold.view.Phase = "consumed_held"
		if !durable {
			hold.view.Phase = "held_process_local"
		}
	}
}

func (c *modelFixtureControl) hasConsumed(scope modelFixtureScope) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	hold := c.entries[modelScopeKey(scope)]
	return hold != nil && hold.view.Consumed
}

func (c *modelFixtureControl) capacity(scope modelFixtureScope) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries) < maxModelFixtureSteps || c.entries[modelScopeKey(scope)] != nil
}

func (c *modelFixtureControl) consumptionStarted(row modelConsumption) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := modelScopeKey(row.scope())
	hold := c.entries[key]
	if hold == nil {
		hold = &modelFixtureHold{}
		c.entries[key] = hold
	}
	hold.view = modelFixtureControlState{Owner: row.Owner, UpdateID: row.UpdateID, Turn: row.Turn,
		Mode: hold.setup.Mode, Phase: "persistence_pending", Consumed: true,
		RequestSHA256: row.RequestSHA256, ResponseSHA256: row.ResponseSHA256}
}

func (c *modelFixtureControl) claim(scope modelFixtureScope) (modelFixtureSelection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hold := c.entries[modelScopeKey(scope)]
	if hold == nil || hold.setup.Mode == "" {
		return modelFixtureSelection{}, nil
	}
	if hold.claimed {
		return modelFixtureSelection{}, errors.New("fixture request already selected")
	}
	if hold.view.Phase != "armed" {
		return modelFixtureSelection{}, nil
	}
	hold.claimed = true
	hold.view.Phase = "matched"
	if hold.setup.Mode == modelHoldBefore {
		hold.view.Phase = "held_before_consume"
	}
	return modelFixtureSelection{mode: hold.setup.Mode, release: hold.release}, nil
}

func (c *modelFixtureControl) finish(scope modelFixtureScope, phase string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if hold := c.entries[modelScopeKey(scope)]; hold != nil {
		if hold.view.Phase == "persistence_unavailable" && phase == "unavailable" {
			return
		}
		hold.view.Phase = phase
		if phase == "persistence_unavailable" {
			hold.view.Durable = false
		}
		if phase != "released" && phase != "persistence_pending" {
			hold.claimed = false
		}
	}
}

func (c *modelFixtureControl) wait(ctx context.Context, scope modelFixtureScope, hold modelFixtureSelection) error {
	if c.cancelled(ctx, scope) {
		return errModelFixtureUnavailable
	}
	timer := time.NewTimer(modelHoldTimeout)
	defer timer.Stop()
	select {
	case action := <-hold.release:
		if c.cancelled(ctx, scope) {
			return errModelFixtureUnavailable
		}
		c.finish(scope, "released")
		err := modelReleaseError(action)
		if err != nil {
			c.finish(scope, "provider_failure")
		}
		return err
	case <-ctx.Done():
		c.cancelled(ctx, scope)
	case <-c.lifetime.Done():
		c.finish(scope, "provider_stopped")
	case <-timer.C:
		if !c.cancelled(ctx, scope) {
			c.finish(scope, "hold_expired")
		}
	}
	return errModelFixtureUnavailable
}

func (c *modelFixtureControl) cancelled(ctx context.Context, scope modelFixtureScope) bool {
	if c.lifetime.Err() != nil {
		c.finish(scope, "provider_stopped")
		return true
	}
	if ctx.Err() != nil {
		c.finish(scope, "request_cancelled")
		return true
	}
	return false
}

func (c *modelFixtureControl) control(w http.ResponseWriter, r *http.Request, body []byte) {
	scope := modelFixtureScope{Owner: r.URL.Query().Get("owner")}
	update, updateErr := strconv.ParseInt(r.URL.Query().Get("update_id"), 10, 64)
	turn, turnErr := strconv.Atoi(r.URL.Query().Get("turn"))
	scope.UpdateID, scope.Turn = update, turn
	if updateErr != nil || turnErr != nil || !validModelScope(scope) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/control/model/state" {
		view, exists := c.view(scope)
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(view)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/control/model/release" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var release struct {
		Action string `json:"action"`
	}
	if strictFixtureJSON(body, &release) != nil ||
		(release.Action != "deliver" && release.Action != "fail" && release.Action != "disconnect") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	view, status := c.release(scope, release.Action)
	w.WriteHeader(status)
	if status == http.StatusOK {
		_ = json.NewEncoder(w).Encode(view)
	}
}

func (c *modelFixtureControl) view(scope modelFixtureScope) (modelFixtureControlState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hold := c.entries[modelScopeKey(scope)]
	if hold == nil {
		return modelFixtureControlState{}, false
	}
	return hold.view, true
}

func (c *modelFixtureControl) release(scope modelFixtureScope, action string) (modelFixtureControlState, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hold := c.entries[modelScopeKey(scope)]
	if hold == nil {
		return modelFixtureControlState{}, http.StatusNotFound
	}
	if c.lifetime.Err() != nil {
		hold.view.Phase = "provider_stopped"
		hold.claimed = false
		return hold.view, http.StatusConflict
	}
	if hold.view.Phase != "held_before_consume" && hold.view.Phase != "consumed_held" &&
		hold.view.Phase != "held_process_local" {
		return modelFixtureControlState{}, http.StatusConflict
	}
	hold.view.Phase = delayStateReleasing
	hold.release <- action
	return hold.view, http.StatusOK
}
