package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

const modelPersistenceTimeout = 2 * time.Second

var errModelFixtureUnavailable = errors.New("fixture provider unavailable")

// Only consumption evidence survives provider restart. No prompt or plan is stored.
type modelConsumption struct {
	Owner          string `json:"owner"`
	UpdateID       int64  `json:"update_id"`
	Turn           int    `json:"turn"`
	RequestSHA256  string `json:"request_sha256"`
	ResponseSHA256 string `json:"response_sha256"`
}

type modelConsumptionSnapshot struct {
	Version int                `json:"version"`
	Rows    []modelConsumption `json:"rows"`
}

func modelScopeKey(scope modelFixtureScope) string {
	return modelFixtureKey(scope.Owner, scope.UpdateID) + ":" + stringTurn(scope.Turn)
}

func validModelScope(scope modelFixtureScope) bool {
	return syntheticFixtureOwner(scope.Owner) && scope.UpdateID > 0 && scope.Turn >= 0 &&
		scope.Turn < maxModelFixtureSteps
}

func (r modelConsumption) scope() modelFixtureScope {
	return modelFixtureScope{Owner: r.Owner, UpdateID: r.UpdateID, Turn: r.Turn}
}

func (f *Fake) modelConsumptionSnapshot() *modelConsumptionSnapshot {
	if len(f.modelConsumed) == 0 {
		return nil
	}
	return &modelConsumptionSnapshot{Version: 1, Rows: f.modelConsumed}
}

func (f *Fake) restoreModelConsumption(snapshot *modelConsumptionSnapshot) error {
	if snapshot == nil {
		return nil
	}
	if snapshot.Version != 1 || len(snapshot.Rows) > maxModelFixtureSteps {
		return errors.New("invalid model consumption state")
	}
	seen := map[string]bool{}
	for _, row := range snapshot.Rows {
		key := modelScopeKey(row.scope())
		if !validModelScope(row.scope()) || !modelDigest(row.RequestSHA256) || !modelDigest(row.ResponseSHA256) ||
			seen[key] {
			return errors.New("invalid model consumption state")
		}
		seen[key] = true
	}
	f.modelConsumed = snapshot.Rows
	for _, row := range snapshot.Rows {
		f.modelControl.restore(row)
	}
	return nil
}

func modelDigest(value string) bool {
	bytes, err := hex.DecodeString(value)
	return err == nil && len(bytes) == sha256.Size && hex.EncodeToString(bytes) == value
}

func (f *Fake) installModelCase(ctx context.Context, value modelFixtureInstall) error {
	for _, row := range f.modelConsumed {
		if row.Owner == value.Owner && row.UpdateID == value.UpdateID {
			return errors.New("fixture identity already consumed")
		}
	}
	if err := f.modelControl.validateInstall(value); err != nil {
		return err
	}
	if err := f.modelFixtures.installContext(ctx, f.modelControl.lifetime, value); err != nil {
		return err
	}
	f.modelControl.install(value)
	return nil
}

// Acquire only this provider's mutation lock; SQL and lock admission share a deadline.
func (f *Fake) modelMutationLock(ctx context.Context) error {
	ticker := time.NewTicker(delayLockPoll)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if f.mu.TryLock() {
			if err := ctx.Err(); err != nil {
				f.mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *modelFixtures) modelLock(ctx context.Context) error {
	ticker := time.NewTicker(delayLockPoll)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.mu.TryLock() {
			if err := ctx.Err(); err != nil {
				m.mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (f *Fake) consumeModelPlan(
	ctx context.Context,
	scope modelFixtureScope,
	input agent.Input,
	hold modelFixtureSelection,
) (agent.Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, modelPersistenceTimeout)
	defer cancel()
	if err := f.modelMutationLock(ctx); err != nil {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	defer f.mu.Unlock()
	if err := f.modelFixtures.modelLock(ctx); err != nil {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	defer f.modelFixtures.mu.Unlock()
	if f.modelControl.cancelled(ctx, scope, hold) {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	if len(f.modelConsumed) >= maxModelFixtureSteps && !f.consumedScope(scope) {
		return agent.Plan{}, errors.New("fixture consumption capacity reached")
	}
	if !f.modelControl.capacity(scope) {
		return agent.Plan{}, errors.New("fixture control capacity reached")
	}
	_, live := f.modelFixtures.cases[modelFixtureKey(scope.Owner, scope.UpdateID)]
	if f.consumedScope(scope) && !live {
		return agent.Plan{}, errors.New("fixture consumed unavailable")
	}
	plan, err := f.modelFixtures.fixturePlanLocked(ctx, f.modelControl.lifetime, scope, input, true)
	if err != nil {
		return plan, err
	}
	request, _ := json.Marshal(input)
	response, _ := json.Marshal(plan)
	requestSHA, responseSHA := sha256.Sum256(request), sha256.Sum256(response)
	row := modelConsumption{Owner: scope.Owner, UpdateID: scope.UpdateID, Turn: scope.Turn,
		RequestSHA256: hex.EncodeToString(requestSHA[:]), ResponseSHA256: hex.EncodeToString(responseSHA[:])}
	f.modelConsumed = append(f.modelConsumed, row)
	f.modelControl.consumptionStarted(row)
	if f.modelControl.cancelled(ctx, scope, hold) {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	if err = f.save(ctx); err != nil {
		f.modelControl.finishSelection(scope, hold, "persistence_unavailable")
		return agent.Plan{}, errModelFixtureUnavailable
	}
	f.modelControl.consumed(row, f.DB != nil)
	if f.modelControl.cancelled(ctx, scope, hold) {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	return plan, nil
}

func (f *Fake) consumedScope(scope modelFixtureScope) bool {
	for _, row := range f.modelConsumed {
		if row.scope() == scope {
			return true
		}
	}
	return false
}

// Selection shares installation's lock so a matched request cannot miss its hold.
// Release the lock before any hold wait; installation and SQL remain independent.
func (f *Fake) selectModelHold(
	ctx context.Context,
	scope modelFixtureScope,
	input agent.Input,
) (modelFixtureSelection, error) {
	ctx, cancel := context.WithTimeout(ctx, modelPersistenceTimeout)
	defer cancel()
	if err := f.modelMutationLock(ctx); err != nil {
		return modelFixtureSelection{}, errModelFixtureUnavailable
	}
	defer f.mu.Unlock()
	if err := f.modelFixtures.modelLock(ctx); err != nil {
		return modelFixtureSelection{}, errModelFixtureUnavailable
	}
	defer f.modelFixtures.mu.Unlock()
	if f.modelControl.cancelled(ctx, scope, modelFixtureSelection{}) {
		return modelFixtureSelection{}, errModelFixtureUnavailable
	}
	if _, err := f.modelFixtures.fixturePlanLocked(ctx, f.modelControl.lifetime, scope, input, false); err != nil {
		if f.modelControl.hasConsumed(scope) {
			return modelFixtureSelection{}, errors.New("fixture consumed unavailable")
		}
		return modelFixtureSelection{}, err
	}
	return f.modelControl.claim(scope)
}

func (f *Fake) fixtureModelPlan(ctx context.Context, scope modelFixtureScope, input agent.Input) (agent.Plan, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(f.modelControl.lifetime, cancel)
	defer func() { stop(); cancel() }()
	if f.modelControl.cancelled(ctx, scope, modelFixtureSelection{}) {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	hold, err := f.selectModelHold(ctx, scope, input)
	if err != nil {
		return agent.Plan{}, err
	}
	if hold.mode == modelHoldBefore {
		if err = f.modelControl.wait(ctx, scope, hold); err != nil {
			return agent.Plan{}, err
		}
	}
	plan, err := f.consumeModelPlan(ctx, scope, input, hold)
	if err != nil {
		if !f.modelControl.cancelled(ctx, scope, hold) {
			f.modelControl.finishSelection(scope, hold, "unavailable")
		}
		return plan, err
	}
	if hold.mode == modelHoldAfter {
		if err = f.modelControl.wait(ctx, scope, hold); err != nil {
			return agent.Plan{}, err
		}
	}
	if f.modelControl.cancelled(ctx, scope, hold) {
		return agent.Plan{}, errModelFixtureUnavailable
	}
	f.modelControl.finishSelection(scope, hold, "response_generated")
	return plan, nil
}

func modelReleaseError(action string) error {
	switch action {
	case "deliver":
		return nil
	case "disconnect":
		return http.ErrAbortHandler
	default:
		return errModelFixtureUnavailable
	}
}
