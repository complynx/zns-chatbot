package bot

import (
	"context"
	"net/http"
	"net/url"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c APIClient) PassCapabilities(ctx context.Context, actor, event string) (passbooking.Capabilities, error) {
	var value passbooking.Capabilities
	err := c.call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/capabilities", nil, &value)
	return value, err
}

func (b *Bot) refreshPassCapabilities(ctx context.Context, owner string, value *agent.RegistrationContext) error {
	events := []string{}
	for _, event := range value.Events {
		events = append(events, event.ID)
	}
	if value.CurrentEvent != "" {
		events = append(events, value.CurrentEvent)
	}
	for _, read := range value.Reads {
		if read.Request.Event != "" {
			events = append(events, read.Request.Event)
		}
	}
	slices.Sort(events)
	value.Capabilities = nil
	for _, event := range slices.Compact(events) {
		capability, err := b.API.PassCapabilities(ctx, owner, event)
		if err != nil {
			return err
		}
		value.Capabilities = append(value.Capabilities, capability)
	}
	return nil
}
