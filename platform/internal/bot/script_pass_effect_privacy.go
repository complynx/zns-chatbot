package bot

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// Check private response data without executing or resuming a committed effect.
// Status-only delivery receipts and owner operation references remain available.
func (b *Bot) scriptPassEffectChanged(
	ctx context.Context,
	owner string,
	call agenthost.ScriptToolRecord,
) (bool, error) {
	request := call.Pass
	if request == nil || request.Menu != nil || request.Batch != nil || request.Name == scriptPassExport ||
		len(call.Outcome.Result) == 0 {
		return false, nil
	}
	var receipt struct {
		Complete bool            `json:"complete"`
		Result   json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(call.Outcome.Result, &receipt); err != nil {
		return false, err
	}
	if !receipt.Complete || len(receipt.Result) == 0 {
		return false, nil
	}
	if request.Command != nil && strings.HasPrefix(request.Name, "passes.registration.") {
		var previous passbooking.Booking
		if err := json.Unmarshal(receipt.Result, &previous); err != nil {
			return false, err
		}
		current, err := b.API.PassBooking(ctx, owner, request.Command.Event)
		if err != nil {
			return true, passMenuFailure(err)
		}
		return !samePassSnapshot(previous, current), nil
	}
	return b.scriptPassEffectAuthority(ctx, owner, request, receipt.Result)
}

func (b *Bot) scriptPassEffectAuthority(
	ctx context.Context,
	owner string,
	request *agenthost.ScriptPassRequest,
	result json.RawMessage,
) (bool, error) {
	var event string
	switch {
	case request.Command != nil:
		event = request.Command.Event
	case request.Assignment != nil:
		event = request.Assignment.Event
	default:
		return true, nil
	}
	changed, err := b.passCapabilityChanged(ctx, owner, event, agenthost.PassToolActions()[request.Name])
	if err != nil || changed {
		return changed, err
	}
	if request.Command != nil {
		switch request.Command.Name {
		case registrationProofAccept, registrationProofReject:
			_, err = b.API.PassPayment(ctx, owner, event, request.Command.Target)
		case passbooking.CommandTakeover, passbooking.CommandReceivedOnly:
			var booking passbooking.Booking
			if decodeErr := json.Unmarshal(result, &booking); decodeErr != nil {
				return false, decodeErr
			}
			_, err = b.API.PassTakeoverTarget(ctx, owner, event, booking.TelegramID)
		}
	}
	if err != nil {
		return true, passMenuFailure(err)
	}
	return false, nil
}
