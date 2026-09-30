package agenthost_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestReceiptObservationEncodeRetainsFreshAuthorities(t *testing.T) {
	t.Parallel()
	const id = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	createdAt := time.Date(2026, time.September, 30, 18, 0, 0, 123456000, time.UTC)
	value := interaction.RegistrationReceiptObservation{ID: id, Complete: true,
		Result: passbooking.Booking{Event: "dance", Owner: "alice", State: "cancelled", Version: 2},
		ReadAuthorities: readsource.Registration([]passbooking.ReadAuthority{
			{Kind: passbooking.ReadCapability, Event: "dance", Action: "invite"},
			{Kind: passbooking.ReadOwnerBooking, Event: "dance", Owner: "alice", Version: 2, CreatedAt: createdAt},
		})}
	call := agenthost.ScriptToolRecord{PassReceiptID: id, Outcome: agent.ScriptToolResult{Name: "passes.resume"}}
	visible, err := (agenthost.ScriptHost{}).EncodeResult(&call, value, "", 16000)
	require.NoError(t, err)
	require.Equal(t, value.ReadAuthorities, call.ResultAuthorities)
	require.NotContains(t, string(visible), "read_authorities")
	require.NotContains(t, string(call.Outcome.Result), "read_authorities")
	refs, err := agenthost.ScriptCallResultAuthorities(call)
	require.NoError(t, err)
	require.Equal(t, value.ReadAuthorities, refs)
	var projected interaction.RegistrationReceiptObservation
	require.NoError(t, json.Unmarshal(visible, &projected))
	require.Equal(t, id, projected.ID)
	require.Equal(t, "cancelled", projected.Result.State)
	require.Empty(t, projected.ReadAuthorities)
}
