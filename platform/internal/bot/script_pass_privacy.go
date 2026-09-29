package bot

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const savedOperationSummaryLimit = 20

func decodePassOperationSummaries(raw json.RawMessage) ([]interaction.RegistrationOperationSummary, bool) {
	var previous []interaction.RegistrationOperationSummary
	err := json.Unmarshal(raw, &previous)
	return previous, err == nil && len(previous) <= savedOperationSummaryLimit
}

func (b *Bot) passOperationSummaryChanged(
	ctx context.Context,
	owner string,
	outcome agent.ScriptToolResult,
) (bool, error) {
	return passOperationSummaryChanged(ctx, owner, outcome, b.registrationOperations().Read)
}

func (b *Bot) scriptPassCallChanged(ctx context.Context, owner string, call agenthost.ScriptToolRecord) (bool, error) {
	changed, effectErr := b.scriptPassEffectChanged(ctx, owner, call)
	if effectErr != nil || changed {
		return changed, effectErr
	}
	changed, readErr := b.scriptPassToolReadChanged(ctx, owner, call)
	if readErr != nil || changed {
		return changed, readErr
	}
	changed, discoveryErr := b.scriptPassDiscoveryChanged(ctx, owner, call.Outcome)
	if discoveryErr != nil || changed {
		return changed, discoveryErr
	}
	previous, err := privatePassRead(call.Outcome)
	if err != nil {
		return false, err
	}
	if previous.Version == 0 {
		return false, nil
	}
	booking, err := b.API.PassBooking(ctx, owner, previous.Event)
	if err != nil {
		return true, passMenuFailure(err)
	}
	if !samePassSnapshot(previous, booking) {
		return true, nil
	}
	return false, nil
}

// A saved owner booking stays private regardless of the event's current dates.
func privatePassRead(outcome agent.ScriptToolResult) (passbooking.Booking, error) {
	if len(outcome.Result) == 0 {
		return passbooking.Booking{}, nil
	}
	switch outcome.Name {
	case scriptPassRead:
		var read agent.RegistrationReadResult
		if err := json.Unmarshal(outcome.Result, &read); err != nil {
			return passbooking.Booking{}, err
		}
		if read.Booking != nil {
			return *read.Booking, nil
		}
	case scriptPassGet:
		var booking passbooking.Booking
		if err := json.Unmarshal(outcome.Result, &booking); err != nil {
			return passbooking.Booking{}, err
		}
		return booking, nil
	}
	return passbooking.Booking{}, nil
}

func passOperationSummaryChanged(
	ctx context.Context,
	owner string,
	outcome agent.ScriptToolResult,
	read func(context.Context, string, derivedmutation.PassOperationQuery) (interaction.RegistrationOperationRead, error),
) (bool, error) {
	if outcome.Name != scriptPassOperations || outcome.Error != "" || len(outcome.Result) == 0 {
		return false, nil
	}
	previous, valid := decodePassOperationSummaries(outcome.Result)
	if !valid {
		return true, nil
	}
	for _, saved := range previous {
		current, err := read(ctx, owner, derivedmutation.PassOperationQuery{ID: saved.ID})
		if err != nil {
			return true, passMenuFailure(err)
		}
		if len(current.Summaries) != 1 {
			return true, nil
		}
		value := current.Summaries[0]
		if value.AdmittedAt.Equal(saved.AdmittedAt) {
			value.AdmittedAt = saved.AdmittedAt
		}
		// List summaries omit item detail and bound the recipient preview.
		if saved.Items == nil {
			value.Items = nil
		}
		if saved.Context != nil && value.Context != nil &&
			len(value.Context.Recipients) > len(saved.Context.Recipients) {
			value.Context.Recipients = value.Context.Recipients[:len(saved.Context.Recipients)]
		}
		if !reflect.DeepEqual(saved, value) {
			return true, nil
		}
	}
	return false, nil
}
