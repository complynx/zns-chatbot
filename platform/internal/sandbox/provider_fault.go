package sandbox

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
)

const (
	providerFaultCaseLimit     = 8
	providerFaultRequestLimit  = 16
	providerFaultLifetimeLimit = 600
	providerFaultAllDelivery   = "all_delivery"
	providerFaultRateLimit     = "rate_limit"
	providerFaultCredential    = "credential"
	providerFaultExhausted     = "exhausted"
	providerFaultExpired       = "expired"
	providerFaultReleased      = "released"
)

type providerFaultSpec struct {
	Mode            string `json:"mode"`
	Method          string `json:"method"`
	Chat            int64  `json:"chat"`
	Thread          int64  `json:"thread"`
	Count           int    `json:"count"`
	LifetimeSeconds int    `json:"lifetime_seconds"`
	RetryAfter      *int64 `json:"retry_after,omitempty"`
}

type providerFaultRequest struct {
	providerFaultSpec
	Case   string `json:"case"`
	Action string `json:"action"`
}

// These records describe a provider rejection prepared before writing HTTP.
// They do not assert client receipt, product completion or a message identity.
type providerFaultConsumption struct {
	Ordinal    int       `json:"ordinal"`
	Method     string    `json:"method"`
	Chat       int64     `json:"chat"`
	Thread     int64     `json:"thread"`
	Status     int       `json:"status"`
	PreparedAt time.Time `json:"prepared_at"`
}

type providerFaultCase struct {
	Case         string                     `json:"case"`
	Selector     providerFaultSpec          `json:"selector"`
	State        string                     `json:"state"`
	Remaining    int                        `json:"remaining"`
	ArmedAt      time.Time                  `json:"armed_at"`
	Deadline     time.Time                  `json:"deadline"`
	Consumptions []providerFaultConsumption `json:"consumptions"`
}

type providerFaultControl struct {
	Cases map[string]providerFaultCase `json:"cases"`
}

func (s providerFaultSpec) valid() bool {
	if s.Count < 1 || s.Count > providerFaultRequestLimit || s.LifetimeSeconds < 1 ||
		s.LifetimeSeconds > providerFaultLifetimeLimit {
		return false
	}
	switch s.Method {
	case "sendMessage", editMessageTextMethod, "sendDocument", providerFaultAllDelivery:
	default:
		return false
	}
	if s.Chat == 0 {
		if s.Thread != 0 {
			return false
		}
	} else if _, ok := destination(strconv.FormatInt(s.Chat, 10)); !ok {
		return false
	}
	if s.Thread != 0 && (s.Chat != forumID || (s.Thread != 101 && s.Thread != 102) || s.Method == "sendDocument") {
		return false
	}
	switch s.Mode {
	case providerFaultRateLimit:
		return true
	case providerFaultCredential:
		return s.Method == providerFaultAllDelivery && s.Chat == 0 && s.Thread == 0 && s.RetryAfter == nil
	default:
		return false
	}
}

func validProviderFaultKey(key string) bool { return key != "" && len(key) <= 64 }

func providerFaultObserved(item providerFaultCase, now time.Time) providerFaultCase {
	if item.State == delayStateArmed && !now.Before(item.Deadline) {
		item.State = providerFaultExpired
	}
	return item
}

func validateProviderFaults(control *providerFaultControl) error {
	if control == nil {
		return nil
	}
	if len(control.Cases) > providerFaultCaseLimit {
		return errors.New("invalid provider fault state")
	}
	active := 0
	for key, item := range control.Cases {
		if !validProviderFaultKey(key) || item.Case != key || !validProviderFaultCase(item) {
			return errors.New("invalid provider fault state")
		}
		if providerFaultObserved(item, time.Now().UTC()).State == delayStateArmed {
			active++
		}
	}
	if active > 1 {
		return errors.New("invalid provider fault state")
	}
	return nil
}

func validProviderFaultCase(item providerFaultCase) bool {
	if !item.Selector.valid() || item.ArmedAt.IsZero() ||
		item.Deadline.Sub(item.ArmedAt) != time.Duration(item.Selector.LifetimeSeconds)*time.Second ||
		item.Remaining < 0 || item.Remaining > item.Selector.Count || len(item.Consumptions) > item.Selector.Count {
		return false
	}
	switch item.State {
	case delayStateArmed:
		if item.Remaining == 0 || item.Remaining+len(item.Consumptions) != item.Selector.Count {
			return false
		}
	case providerFaultExhausted:
		if item.Remaining != 0 || len(item.Consumptions) != item.Selector.Count {
			return false
		}
	case providerFaultReleased:
		if item.Remaining != 0 {
			return false
		}
	default:
		return false
	}
	previous := item.ArmedAt
	for index, consumed := range item.Consumptions {
		if consumed.Ordinal != index+1 || !providerFaultMatches(item.Selector, consumed.Method, consumed.Chat, consumed.Thread) ||
			consumed.Status != providerFaultStatus(item.Selector) || consumed.PreparedAt.Before(previous) ||
			!consumed.PreparedAt.Before(item.Deadline) {
			return false
		}
		previous = consumed.PreparedAt
	}
	return true
}

func providerFaultMatches(spec providerFaultSpec, method string, chat, thread int64) bool {
	if method != "sendMessage" && method != editMessageTextMethod && method != "sendDocument" {
		return false
	}
	if _, known := destination(strconv.FormatInt(chat, 10)); !known {
		return false
	}
	if thread != 0 && (chat != forumID || (thread != 101 && thread != 102) || method == "sendDocument") {
		return false
	}
	return (spec.Method == providerFaultAllDelivery || spec.Method == method) &&
		(spec.Chat == 0 || spec.Chat == chat) && (spec.Thread == 0 || spec.Thread == thread)
}

func providerFaultStatus(spec providerFaultSpec) int {
	if spec.Mode == providerFaultCredential {
		return http.StatusUnauthorized
	}
	return http.StatusTooManyRequests
}

// The caller holds the fake mutex. Expiry is a read-only observation.
func (f *Fake) activeProviderFault(now time.Time) bool {
	if f.providerFaults == nil {
		return false
	}
	for _, item := range f.providerFaults.Cases {
		if providerFaultObserved(item, now).State == delayStateArmed {
			return true
		}
	}
	return false
}

func (f *Fake) providerFaultDelayBusy() bool {
	if f.providerFaultEditArm {
		return true
	}
	if f.delay == nil {
		return false
	}
	f.delay.mu.Lock()
	defer f.delay.mu.Unlock()
	switch f.delay.state {
	case "idle", "completed", "response_lost", "expired_unresolved", "unresolved_response", delayStateInvalidated:
		return false
	default:
		return true
	}
}

// Reserve arm custody without holding the fake mutex across journal I/O.
func (f *Fake) providerFaultEditGuard() (func(), bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.providerFaultEditArm || f.activeProviderFault(time.Now().UTC()) || f.fault != "" && f.fault != noFault {
		return nil, false
	}
	f.providerFaultEditArm = true
	return func() {
		f.mu.Lock()
		f.providerFaultEditArm = false
		f.mu.Unlock()
	}, true
}

func (f *Fake) cloneProviderFaults() *providerFaultControl {
	copyControl := &providerFaultControl{Cases: make(map[string]providerFaultCase)}
	if f.providerFaults != nil {
		for key, item := range f.providerFaults.Cases {
			copyControl.Cases[key] = item
		}
	}
	return copyControl
}

func (f *Fake) saveProviderFault(ctx context.Context, item providerFaultCase) error {
	previous := f.providerFaults
	next := f.cloneProviderFaults()
	next.Cases[item.Case] = item
	f.providerFaults = next
	if err := f.save(ctx); err != nil {
		f.providerFaults = previous
		return err
	}
	return nil
}

func (f *Fake) providerFaultRead(w http.ResponseWriter, r *http.Request) {
	if !f.ingressControlAuthorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	query := r.URL.Query()["case"]
	if len(query) != 1 || !validProviderFaultKey(query[0]) {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if f.providerFaults == nil {
		http.NotFound(w, r)
		return
	}
	item, exists := f.providerFaults.Cases[query[0]]
	if !exists {
		http.NotFound(w, r)
		return
	}
	api.JSON(w, http.StatusOK, providerFaultObserved(item, time.Now().UTC()))
}

func (f *Fake) providerFaultCommand(w http.ResponseWriter, r *http.Request) {
	if !f.ingressControlAuthorized(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, delayControlBodyLimit)
	var request providerFaultRequest
	if api.Decode(w, r, &request) != nil || !validProviderFaultKey(request.Case) {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	item, changed, status := f.applyProviderFault(request)
	if status != http.StatusOK {
		api.JSON(w, status, nil)
		return
	}
	if changed {
		if err := f.saveProviderFault(r.Context(), item); err != nil {
			api.JSON(w, http.StatusServiceUnavailable, nil)
			return
		}
	}
	api.JSON(w, http.StatusOK, providerFaultObserved(item, time.Now().UTC()))
}

func (f *Fake) applyProviderFault(request providerFaultRequest) (providerFaultCase, bool, int) {
	var item providerFaultCase
	var exists bool
	if f.providerFaults != nil {
		item, exists = f.providerFaults.Cases[request.Case]
	}
	switch request.Action {
	case "arm":
		return f.armProviderFault(request, item, exists)
	case "release":
		if !exists || !reflect.DeepEqual(request.providerFaultSpec, providerFaultSpec{}) {
			return item, false, http.StatusConflict
		}
		if item.State == providerFaultReleased {
			return item, false, http.StatusOK
		}
		item.State, item.Remaining = providerFaultReleased, 0
		return item, true, http.StatusOK
	default:
		return item, false, http.StatusBadRequest
	}
}

func (f *Fake) armProviderFault(
	request providerFaultRequest,
	item providerFaultCase,
	exists bool,
) (providerFaultCase, bool, int) {
	if !request.providerFaultSpec.valid() {
		return item, false, http.StatusBadRequest
	}
	if exists {
		if !reflect.DeepEqual(item.Selector, request.providerFaultSpec) {
			return item, false, http.StatusConflict
		}
		return item, false, http.StatusOK
	}
	if f.activeProviderFault(time.Now().UTC()) || f.providerFaultDelayBusy() || f.fault != "" && f.fault != noFault ||
		f.providerFaults != nil && len(f.providerFaults.Cases) >= providerFaultCaseLimit {
		return item, false, http.StatusConflict
	}
	now := time.Now().UTC()
	item = providerFaultCase{Case: request.Case, Selector: request.providerFaultSpec, State: delayStateArmed,
		Remaining: request.Count, ArmedAt: now, Deadline: now.Add(time.Duration(request.LifetimeSeconds) * time.Second),
		Consumptions: []providerFaultConsumption{}}
	return item, true, http.StatusOK
}

// Called after ordinary payload admission, with the fake mutex held.
func (f *Fake) rejectProviderFault(w http.ResponseWriter, r *http.Request, method string, chat, thread int64) bool {
	if f.delay == nil || f.providerFaults == nil {
		return false
	}
	now := time.Now().UTC()
	for _, current := range f.providerFaults.Cases {
		if providerFaultObserved(current, now).State != delayStateArmed ||
			!providerFaultMatches(current.Selector, method, chat, thread) {
			continue
		}
		item := current
		item.Remaining--
		if item.Remaining == 0 {
			item.State = providerFaultExhausted
		}
		item.Consumptions = append(
			append([]providerFaultConsumption{}, current.Consumptions...),
			providerFaultConsumption{
				Ordinal: len(current.Consumptions) + 1, Method: method, Chat: chat, Thread: thread,
				Status: providerFaultStatus(current.Selector), PreparedAt: now,
			},
		)
		if err := f.saveProviderFault(r.Context(), item); err != nil {
			tgError(w, http.StatusServiceUnavailable, "state unavailable")
			return true
		}
		status, description := providerFaultStatus(item.Selector), "Too Many Requests"
		if status == http.StatusUnauthorized {
			description = "Unauthorized"
		}
		envelope := map[string]any{"ok": false, "error_code": status, "description": description}
		if item.Selector.RetryAfter != nil {
			envelope["parameters"] = map[string]int64{"retry_after": *item.Selector.RetryAfter}
		}
		api.JSON(w, status, envelope)
		return true
	}
	return false
}
