package sandbox

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const (
	callbackAliceIndex = iota
	callbackBobIndex
	callbackVisitorIndex
	callbackUserCount
)

const callbackReceiptLimit = 64
const callbackIdentifierLimit = 32
const callbackObservationBodyLimit = 4096

// Evidence is bounded per synthetic user and lasts only for this provider process.
// It records actual provider calls, not product delivery or browser receipt.
type callbackReceipt struct {
	Sequence   uint64    `json:"sequence"`
	CallbackID string    `json:"callback_query_id"`
	BotID      int64     `json:"bot_id"`
	UserID     int64     `json:"user_id"`
	MessageID  int64     `json:"message_id"`
	Status     int       `json:"response_status"`
	Result     bool      `json:"result"`
	ObservedAt time.Time `json:"observed_at"`
}

type callbackBinding struct {
	CallbackID string
	UserID     int64
	MessageID  int64
}

type callbackUserEvidence struct {
	Total     uint64
	Truncated bool
	Bindings  []callbackBinding
	Receipts  []callbackReceipt
}

type callbackEvidence struct {
	Users [callbackUserCount]callbackUserEvidence
}

func callbackUserIndex(user int64) (int, bool) {
	owner, ok := identity.Subject(user)
	if !ok {
		return 0, false
	}
	switch owner {
	case "alice":
		return callbackAliceIndex, true
	case "bob":
		return callbackBobIndex, true
	case "visitor":
		return callbackVisitorIndex, true
	default:
		return 0, false
	}
}

// Bind the successful getUpdates batch before its bytes can reach the caller.
func (f *Fake) observeDeliveredCallbacks(batch []telegram.Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, update := range batch {
		callback := update.Callback
		if callback == nil || len(callback.ID) == 0 || len(callback.ID) > callbackIdentifierLimit {
			continue
		}
		index, ok := callbackUserIndex(callback.From.ID)
		if !ok || callback.Message.Chat.ID != callback.From.ID {
			continue
		}
		state := &f.callbackEvidence.Users[index]
		duplicate := false
		for _, binding := range state.Bindings {
			if binding.CallbackID == callback.ID {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if len(state.Bindings) == callbackReceiptLimit {
			state.Bindings = state.Bindings[1:]
			state.Truncated = true
		}
		state.Bindings = append(state.Bindings, callbackBinding{callback.ID, callback.From.ID, callback.Message.ID})
	}
}

// Observation never changes the existing answerCallbackQuery response policy.
func (f *Fake) answerCallbackQuery(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CallbackID string `json:"callback_query_id"`
	}
	body, readErr := io.ReadAll(io.LimitReader(r.Body, callbackObservationBodyLimit+1))
	valid := readErr == nil && len(body) <= callbackObservationBodyLimit &&
		json.Unmarshal(body, &request) == nil &&
		len(request.CallbackID) > 0 && len(request.CallbackID) <= callbackIdentifierLimit
	tgOK(w, true)
	if valid {
		f.observeCallbackResponse(request.CallbackID)
	}
}

func (f *Fake) observeCallbackResponse(callbackID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.callbackEvidence.Users {
		state := &f.callbackEvidence.Users[index]
		for _, binding := range state.Bindings {
			if binding.CallbackID != callbackID {
				continue
			}
			state.Total++
			if len(state.Receipts) == callbackReceiptLimit {
				state.Receipts = state.Receipts[1:]
				state.Truncated = true
			}
			state.Receipts = append(state.Receipts, callbackReceipt{
				Sequence: state.Total, CallbackID: callbackID, BotID: fakeBotID,
				UserID: binding.UserID, MessageID: binding.MessageID,
				Status: http.StatusOK, Result: true, ObservedAt: time.Now().UTC(),
			})
			return
		}
	}
}

// Actor is trusted synthetic operator metadata, not end-user authentication.
func (f *Fake) callbackReceiptState(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	query := r.URL.Query()["user"]
	if len(query) != 1 {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	user, err := strconv.ParseInt(query[0], 10, 64)
	index, known := callbackUserIndex(user)
	owner, _ := identity.Subject(user)
	if err != nil || !known || strconv.FormatInt(user, 10) != query[0] {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if r.Header.Get("X-Sandbox-Actor") != owner {
		api.JSON(w, http.StatusForbidden, nil)
		return
	}
	f.mu.Lock()
	state := f.callbackEvidence.Users[index]
	receipts := append([]callbackReceipt{}, state.Receipts...)
	f.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	api.JSON(w, http.StatusOK, struct {
		Scope     string            `json:"scope"`
		UserID    int64             `json:"user_id"`
		Total     uint64            `json:"total_observed"`
		Truncated bool              `json:"truncated"`
		Receipts  []callbackReceipt `json:"receipts"`
	}{"current_provider_process_response_generated", user, state.Total, state.Truncated, receipts})
}
