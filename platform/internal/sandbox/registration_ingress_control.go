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
	"slices"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const ingressControlCases = 8
const ingressControlBytes = 1 << 20
const ingressHoldLimit = 10 * time.Second
const ingressReleased = "original_released"
const ingressOriginalPending = "original_pending"
const ingressReplayPending = "pending"
const ingressReplayConsumed = "replay_consumed"
const ingressReplayExpired = "replay_expired"
const ingressHeld = "held"
const ingressDelivered = "delivered"
const ingressExpired = "expired"

// These receipts belong to the external provider, not product intake or ranks.
// A replay is one exact duplicate response, never a new Telegram update.
type registrationIngressControl struct {
	Cases map[string]registrationIngressCase `json:"cases"`
}

type registrationIngressCase struct {
	User              int64     `json:"user"`
	State             string    `json:"state"`
	Deadline          time.Time `json:"deadline"`
	ArmedAt           time.Time `json:"armed_at"`
	CapturedAt        time.Time `json:"captured_at"`
	CustodyFinishedAt time.Time `json:"custody_finished_at"`
	Response          []byte    `json:"response,omitempty"`
	SHA256            string    `json:"sha256"`
	Replay            string    `json:"replay"`
	ReplayArmedAt     time.Time `json:"replay_armed_at"`
	ReplayDeadline    time.Time `json:"replay_deadline"`
	ReplayFinishedAt  time.Time `json:"replay_finished_at"`
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
		if item.State == delayStateArmed || item.State == ingressHeld || item.State == ingressReleased {
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
	case delayStateArmed, ingressExpired, ingressHeld, ingressReleased, ingressOriginalPending, ingressDelivered:
	default:
		return errors.New("invalid registration provider receipt state")
	}
	if item.State == ingressOriginalPending && item.CustodyFinishedAt.Before(item.Deadline) ||
		item.State != ingressOriginalPending && item.State != ingressDelivered && !item.CustodyFinishedAt.IsZero() ||
		item.State == ingressDelivered && !item.CustodyFinishedAt.IsZero() &&
			item.CustodyFinishedAt.Before(item.Deadline) {
		return errors.New("invalid registration provider custody timeline")
	}
	if item.State == delayStateArmed || item.State == ingressExpired {
		if len(item.Response) != 0 || item.SHA256 != "" || !item.CapturedAt.IsZero() {
			return errors.New("armed registration provider receipt has content")
		}
		return validateIngressReplay(item)
	}
	if err := validateIngressOriginal(item); err != nil {
		return err
	}
	return validateIngressReplay(item)
}

func validateIngressReplay(item registrationIngressCase) error {
	if item.Replay == "" {
		if !item.ReplayArmedAt.IsZero() || !item.ReplayDeadline.IsZero() || !item.ReplayFinishedAt.IsZero() {
			return errors.New("unarmed registration provider replay has timestamps")
		}
		return nil
	}
	if item.State != ingressDelivered || item.ReplayArmedAt.IsZero() || item.ReplayArmedAt.Before(item.CapturedAt) ||
		!item.ReplayDeadline.After(
			item.ReplayArmedAt,
		) || item.ReplayDeadline.Sub(item.ReplayArmedAt) > ingressHoldLimit {
		return errors.New("invalid registration provider replay binding")
	}
	switch item.Replay {
	case ingressReplayPending:
		if item.ReplayFinishedAt.IsZero() {
			return nil
		}
	case ingressReplayConsumed:
		if !item.ReplayFinishedAt.Before(item.ReplayArmedAt) && item.ReplayFinishedAt.Before(item.ReplayDeadline) {
			return nil
		}
	case ingressReplayExpired:
		if !item.ReplayFinishedAt.Before(item.ReplayDeadline) {
			return nil
		}
	}
	return errors.New("invalid registration provider replay state")
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
		) || item.SHA256 != hex.EncodeToString(digest[:]) || item.CapturedAt.IsZero() ||
		item.CapturedAt.Before(item.ArmedAt) || !item.CapturedAt.Before(item.Deadline) {
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
	if err := f.retireIngressCustody(r.Context()); err != nil {
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

// Finite arm/replay custody expires without polling; original responses remain unchanged.
func (f *Fake) retireIngressCustody(ctx context.Context) error {
	value := f.cloneIngressControl()
	changed := false
	now := time.Now().UTC()
	for key, item := range value.Cases {
		updated := expireIngressCustody(item, now)
		if updated.State != item.State || updated.Replay != item.Replay {
			value.Cases[key] = updated
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

func expireIngressCustody(item registrationIngressCase, now time.Time) registrationIngressCase {
	if item.State == delayStateArmed && !now.Before(item.Deadline) {
		item.State = ingressExpired
	}
	if (item.State == ingressHeld || item.State == ingressReleased) && !now.Before(item.Deadline) {
		item.State, item.CustodyFinishedAt = ingressOriginalPending, now
	}
	if item.Replay == ingressReplayPending && !now.Before(item.ReplayDeadline) {
		item.Replay, item.ReplayFinishedAt = ingressReplayExpired, now
	}
	return item
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
	if err := f.retireIngressCustody(r.Context()); err != nil {
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
			(item.State == ingressHeld || item.State == ingressReleased || item.State == ingressOriginalPending ||
				item.State == ingressDelivered) && request.HoldSeconds == 0
		if valid && item.State == ingressHeld {
			item.State = ingressReleased
		}
	case "replay":
		valid = exists && request.User == item.User && request.SHA256 != "" && request.SHA256 == item.SHA256 &&
			item.State == ingressDelivered && request.HoldSeconds == 0
		if valid && item.Replay == "" {
			item.Replay = ingressReplayPending
			item.ReplayArmedAt = time.Now().UTC()
			item.ReplayDeadline = item.ReplayArmedAt.Add(ingressHoldLimit)
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
func (f *Fake) registrationIngressResponse(batch []telegram.Update, offset int64) ([]byte, error) {
	if f.delay == nil || f.registrationIngress == nil {
		return nil, nil
	}
	value := f.cloneIngressControl()
	now := time.Now().UTC()
	for key, item := range value.Cases {
		value.Cases[key] = expireIngressCustody(item, now)
	}
	keys := ingressDeliveryOrder(value)
	for _, key := range keys {
		item := value.Cases[key]
		var updated registrationIngressCase
		var response []byte
		var err error
		switch {
		case item.Replay == ingressReplayPending:
			updated, response, err = ingressReplayResponse(item, offset, now)
		case item.State != delayStateArmed || ingressPreviousAcknowledged(value, offset):
			updated, response, err = ingressOriginalResponse(item, batch)
		default:
			updated = item
		}
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

// Retained originals are delivered before a newer barrier can capture another batch.
func ingressDeliveryOrder(value *registrationIngressControl) []string {
	keys := make([]string, 0, len(value.Cases))
	for key := range value.Cases {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right string) int {
		a, b := value.Cases[left], value.Cases[right]
		if a.State == ingressOriginalPending && b.State != ingressOriginalPending {
			return -1
		}
		if b.State == ingressOriginalPending && a.State != ingressOriginalPending {
			return 1
		}
		if order := a.CapturedAt.Compare(b.CapturedAt); order != 0 {
			return order
		}
		return strings.Compare(left, right)
	})
	return keys
}

func ingressPreviousAcknowledged(value *registrationIngressControl, offset int64) bool {
	for _, item := range value.Cases {
		if len(item.Response) == 0 {
			continue
		}
		acknowledged, err := ingressBatchAcknowledged(item.Response, offset)
		if err != nil || !acknowledged {
			return false
		}
	}
	return true
}

func ingressReplayResponse(
	item registrationIngressCase,
	offset int64,
	now time.Time,
) (registrationIngressCase, []byte, error) {
	acknowledged, err := ingressBatchAcknowledged(item.Response, offset)
	if err != nil || !acknowledged {
		return item, nil, err
	}
	item.Replay, item.ReplayFinishedAt = ingressReplayConsumed, now
	return item, item.Response, nil
}

func ingressBatchAcknowledged(response []byte, offset int64) (bool, error) {
	var envelope struct {
		Result []telegram.Update `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return false, err
	}
	for _, update := range envelope.Result {
		if update.ID >= offset {
			return false, nil
		}
	}
	return true, nil
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
	item = expireIngressCustody(item, now)
	switch item.State {
	case ingressHeld:
		return item, []byte("{\"ok\":true,\"result\":[]}\n"), nil
	case ingressReleased, ingressOriginalPending:
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
