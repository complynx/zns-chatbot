package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const ingressControlCases = 8
const ingressControlBytes = 1 << 20
const ingressHoldLimit = 10 * time.Second
const ingressReleased = "original_released"
const ingressReplayPending = "pending"
const ingressReplayConsumed = "replay_consumed"
const ingressHeld = "held"
const ingressDelivered = "delivered"
const ingressExpired = "expired"

// These receipts belong to the external provider, not product intake or ranks.
// A replay is one exact duplicate response, never a new Telegram update.
type registrationIngressControl struct {
	Cases map[string]registrationIngressCase `json:"cases"`
}

type registrationIngressCase struct {
	User       int64     `json:"user"`
	State      string    `json:"state"`
	Deadline   time.Time `json:"deadline"`
	ArmedAt    time.Time `json:"armed_at"`
	CapturedAt time.Time `json:"captured_at"`
	Response   []byte    `json:"response,omitempty"`
	SHA256     string    `json:"sha256"`
	Replay     string    `json:"replay"`
}

type registrationIngressRequest struct {
	Case        string `json:"case"`
	Action      string `json:"action"`
	User        int64  `json:"user"`
	HoldSeconds int    `json:"hold_seconds"`
	SHA256      string `json:"sha256"`
}

func validateRegistrationIngress(value *registrationIngressControl) error {
	if value == nil {
		return nil
	}
	if len(value.Cases) > ingressControlCases {
		return errors.New("registration provider receipt limit exceeded")
	}
	total := 0
	active := 0
	for key, item := range value.Cases {
		if err := validateIngressCase(key, item); err != nil {
			return err
		}
		if item.State != ingressDelivered && item.State != ingressExpired {
			active++
		}
		if item.Replay == ingressReplayPending {
			active++
		}
		total += len(item.Response)
	}
	if active > 1 || total > ingressControlBytes {
		return errors.New("registration provider receipt budget exceeded")
	}
	return nil
}

func validateIngressCase(key string, item registrationIngressCase) error {
	_, known := identity.Subject(item.User)
	if key == "" || len(key) > 64 || !known || item.ArmedAt.IsZero() ||
		!item.Deadline.After(item.ArmedAt) || item.Deadline.Sub(item.ArmedAt) > ingressHoldLimit {
		return errors.New("invalid registration provider receipt binding")
	}
	switch item.State {
	case delayStateArmed, ingressExpired, ingressHeld, ingressReleased, ingressDelivered:
	default:
		return errors.New("invalid registration provider receipt state")
	}
	if item.Replay != "" && item.Replay != ingressReplayPending && item.Replay != ingressReplayConsumed ||
		item.Replay != "" && item.State != ingressDelivered {
		return errors.New("invalid registration provider replay state")
	}
	if item.State == delayStateArmed || item.State == ingressExpired {
		if len(item.Response) != 0 || item.SHA256 != "" || !item.CapturedAt.IsZero() {
			return errors.New("armed registration provider receipt has content")
		}
		return nil
	}
	return validateIngressOriginal(item)
}

func validateIngressOriginal(item registrationIngressCase) error {
	var envelope struct {
		OK     bool              `json:"ok"`
		Result []telegram.Update `json:"result"`
	}
	digest := sha256.Sum256(item.Response)
	if json.Unmarshal(item.Response, &envelope) != nil || !envelope.OK || len(envelope.Result) == 0 ||
		!ingressBatchOwner(
			envelope.Result,
			item.User,
		) || item.SHA256 != hex.EncodeToString(digest[:]) || item.CapturedAt.IsZero() {
		return errors.New("invalid registration provider original response")
	}
	return nil
}

func (f *Fake) ingressControlAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if f.delay == nil {
		http.NotFound(w, r)
		return false
	}
	if r.Header.Get("X-Sandbox") != "1" || subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("X-R104-Control")), []byte(f.delay.key)) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	return true
}

func (f *Fake) registrationIngressRead(w http.ResponseWriter, r *http.Request) {
	if !f.ingressControlAuthorized(w, r) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.retireIngressArms(r.Context()); err != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	item, ok := f.ingressCase(r.URL.Query().Get("case"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	api.JSON(w, http.StatusOK, item)
}

func (f *Fake) ingressCase(key string) (registrationIngressCase, bool) {
	if f.registrationIngress == nil {
		return registrationIngressCase{}, false
	}
	item, ok := f.registrationIngress.Cases[key]
	return item, ok
}

func (f *Fake) cloneIngressControl() *registrationIngressControl {
	value := &registrationIngressControl{Cases: make(map[string]registrationIngressCase)}
	if f.registrationIngress != nil {
		maps.Copy(value.Cases, f.registrationIngress.Cases)
	}
	return value
}

// Uncaptured custody expires even without polling; captured originals remain replayable.
func (f *Fake) retireIngressArms(ctx context.Context) error {
	value := f.cloneIngressControl()
	changed := false
	now := time.Now().UTC()
	for key, item := range value.Cases {
		if item.State == delayStateArmed && !now.Before(item.Deadline) {
			item.State = ingressExpired
			value.Cases[key] = item
			changed = true
		}
	}
	if !changed {
		return nil
	}
	previous := f.registrationIngress
	f.registrationIngress = value
	if err := f.save(ctx); err != nil {
		f.registrationIngress = previous
		return err
	}
	return nil
}

func (f *Fake) registrationIngressCommand(w http.ResponseWriter, r *http.Request) {
	if !f.ingressControlAuthorized(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, delayControlBodyLimit)
	var request registrationIngressRequest
	if api.Decode(w, r, &request) != nil || request.Case == "" ||
		len(request.Case) > 64 {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.retireIngressArms(r.Context()); err != nil {
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	previous := f.registrationIngress
	value := f.cloneIngressControl()
	item, valid := applyIngressRequest(value, request)
	if !valid {
		api.JSON(w, http.StatusConflict, nil)
		return
	}
	value.Cases[request.Case] = item
	if validateRegistrationIngress(value) != nil {
		api.JSON(w, http.StatusConflict, nil)
		return
	}
	f.registrationIngress = value
	if err := f.save(r.Context()); err != nil {
		f.registrationIngress = previous
		api.JSON(w, http.StatusServiceUnavailable, nil)
		return
	}
	api.JSON(w, http.StatusOK, item)
}

func applyIngressRequest(
	value *registrationIngressControl,
	request registrationIngressRequest,
) (registrationIngressCase, bool) {
	item, exists := value.Cases[request.Case]
	valid := false
	switch request.Action {
	case "arm":
		_, known := identity.Subject(request.User)
		if !exists && known && request.SHA256 == "" && request.HoldSeconds > 0 &&
			request.HoldSeconds <= int(ingressHoldLimit/time.Second) && len(value.Cases) < ingressControlCases {
			armed := time.Now().UTC()
			item = registrationIngressCase{User: request.User, State: delayStateArmed, ArmedAt: armed,
				Deadline: armed.Add(time.Duration(request.HoldSeconds) * time.Second)}
			valid = true
		}
	case "release":
		valid = exists && request.User == item.User && request.SHA256 != "" && request.SHA256 == item.SHA256 &&
			(item.State == ingressHeld || item.State == ingressReleased || item.State == ingressDelivered) && request.HoldSeconds == 0
		if valid && item.State == ingressHeld {
			item.State = ingressReleased
		}
	case "replay":
		valid = exists && request.User == item.User && request.SHA256 != "" && request.SHA256 == item.SHA256 &&
			item.State == ingressDelivered && request.HoldSeconds == 0
		if valid && item.Replay == "" {
			item.Replay = ingressReplayPending
		}
	}
	return item, valid
}

func ingressBatchOwner(batch []telegram.Update, user int64) bool {
	for _, update := range batch {
		if update.Message != nil && update.Message.From.ID == user ||
			update.Callback != nil && update.Callback.From.ID == user {
			return true
		}
	}
	return false
}

// The full original batch is held to preserve getUpdates ordering. New inputs
// may enqueue while held, but the global Telegram poll is not actor-parallel.
func (f *Fake) registrationIngressResponse(batch []telegram.Update) ([]byte, error) {
	if f.delay == nil || f.registrationIngress == nil {
		return nil, nil
	}
	value := f.cloneIngressControl()
	for key, item := range value.Cases {
		if item.Replay == ingressReplayPending {
			item.Replay = ingressReplayConsumed
			value.Cases[key] = item
			f.registrationIngress = value
			return item.Response, nil
		}
		updated, response, err := ingressOriginalResponse(item, batch)
		if err != nil {
			return nil, err
		}
		value.Cases[key] = updated
		if response != nil {
			if err = validateRegistrationIngress(value); err != nil {
				return nil, err
			}
			f.registrationIngress = value
			return response, nil
		}
	}
	f.registrationIngress = value
	return nil, nil
}

func ingressOriginalResponse(
	item registrationIngressCase,
	batch []telegram.Update,
) (registrationIngressCase, []byte, error) {
	now := time.Now().UTC()
	if item.State == delayStateArmed && !now.Before(item.Deadline) {
		item.State = ingressExpired
	}
	if item.State == delayStateArmed && ingressBatchOwner(batch, item.User) {
		var buffer bytes.Buffer
		if err := json.NewEncoder(&buffer).Encode(map[string]any{"ok": true, "result": batch}); err != nil {
			return item, nil, err
		}
		item.Response = buffer.Bytes()
		digest := sha256.Sum256(item.Response)
		item.SHA256 = hex.EncodeToString(digest[:])
		item.CapturedAt = now
		item.State = ingressHeld
	}
	if item.State == ingressHeld && !now.Before(item.Deadline) {
		item.State = ingressReleased
	}
	switch item.State {
	case ingressHeld:
		return item, []byte("{\"ok\":true,\"result\":[]}\n"), nil
	case ingressReleased:
		item.State = ingressDelivered
		return item, item.Response, nil
	default:
		return item, nil, nil
	}
}

func (f *Fake) writeIngressResponse(w http.ResponseWriter, r *http.Request, response []byte) {
	var envelope struct {
		Result []telegram.Update `json:"result"`
	}
	if json.Unmarshal(response, &envelope) != nil {
		tgError(w, http.StatusServiceUnavailable, "state unavailable")
		return
	}
	if len(envelope.Result) == 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(emptyPollDelay):
		}
	}
	f.observeDeliveredCallbacks(envelope.Result)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(response)
}
