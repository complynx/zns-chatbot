package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type privilegedReadClient struct {
	roles            core.PrivilegedReadCapabilities
	page             core.ReadPage[core.PrivilegedReadEvent]
	failure          error
	capabilityOwners []string
	eventOwners      []string
	cursors          []string
}

func (c *privilegedReadClient) PrivilegedReadCapabilities(
	_ context.Context, owner string,
) (core.PrivilegedReadCapabilities, error) {
	c.capabilityOwners = append(c.capabilityOwners, owner)
	return c.roles, c.failure
}

func (c *privilegedReadClient) PrivilegedReadEvents(
	_ context.Context, owner, cursor string,
) (core.ReadPage[core.PrivilegedReadEvent], error) {
	c.eventOwners = append(c.eventOwners, owner)
	c.cursors = append(c.cursors, cursor)
	return c.page, c.failure
}

type livePrivilegedCatalog struct {
	liveScriptCatalog

	privileged PrivilegedReadHost
}

func (c *livePrivilegedCatalog) Privileged(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	return c.privileged.Entries(ctx, owner)
}

func TestPrivilegedCatalogRoleUnionAndSchemas(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		roles core.PrivilegedReadCapabilities
		names []string
	}{
		{name: "none"},
		{name: "payments", roles: core.PrivilegedReadCapabilities{PaymentReads: true},
			names: []string{hostPrivilegesEvents, hostPassesPaymentsQueue, hostPassesPaymentsHistory}},
		{name: "practitioner", roles: core.PrivilegedReadCapabilities{PractitionerReads: true},
			names: []string{hostPrivilegesEvents, hostMassagePractitionerSchedule,
				hostMassagePractitionerPreferences, hostMassagePractitionerBookings}},
		{name: "both", roles: core.PrivilegedReadCapabilities{PaymentReads: true, PractitionerReads: true},
			names: []string{hostPrivilegesEvents, hostPassesPaymentsQueue, hostPassesPaymentsHistory,
				hostMassagePractitionerSchedule, hostMassagePractitionerPreferences, hostMassagePractitionerBookings}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &privilegedReadClient{roles: test.roles}
			entries, err := (PrivilegedReadHost{Client: client,
				Binding: ScriptToolEntry{ResultLimit: 32 << 10}}).Entries(t.Context(), "owner")
			require.NoError(t, err)
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 32<<10, entry.ResultLimit)
				assertPrivilegedSchema(t, entry.Descriptor)
			}
			require.Equal(t, test.names, names)
			require.Equal(t, []string{"owner"}, client.capabilityOwners)
			require.Empty(t, client.eventOwners)
		})
	}
}

func TestPrivilegedCatalogLiveDiscoveryAndDispatch(t *testing.T) {
	t.Parallel()
	client := &privilegedReadClient{roles: core.PrivilegedReadCapabilities{PaymentReads: true}}
	denied := &core.ProblemError{Status: 403, Code: "event_payment_forbidden"}
	prepared, executed := false, false
	binding := ScriptToolEntry{ResultLimit: 32 << 10,
		Prepare: func(_ context.Context, owner string, _ int64, call scriptclient.ToolCall,
			_ agent.Input,
		) (ScriptToolRecord, error) {
			require.Equal(t, "owner", owner)
			prepared = true
			return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
		},
		Execute: func(_ context.Context, owner string, _ scriptclient.ToolCall, _ ScriptToolRecord,
			_ *agent.Input,
		) (any, error) {
			require.Equal(t, "owner", owner)
			executed = true
			return nil, denied
		},
	}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: &livePrivilegedCatalog{
		privileged: PrivilegedReadHost{Client: client, Binding: binding},
	}}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	client.roles = core.PrivilegedReadCapabilities{PractitionerReads: true}
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Contains(t, string(list), hostPrivilegesEvents)
	require.NotContains(t, string(list), hostPassesPaymentsQueue)
	require.NotContains(t, string(list), hostMassagePractitionerBookings)
	_, err = host.Registry.Resolve(ctx, "owner", hostMassagePractitionerBookings)
	require.Error(t, err, "new role cannot broaden running VM")
	_, err = host.Registry.Resolve(ctx, "owner", hostPassesPaymentsQueue)
	require.Error(t, err, "known-name call must respect current revocation")
	require.False(t, prepared)
	client.roles.PaymentReads = true
	help, err := host.discover(ctx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"passes.payments.queue"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "currently a payment administrator")
	entry, err := host.Registry.Resolve(ctx, "owner", hostPassesPaymentsQueue)
	require.NoError(t, err)
	call := scriptclient.ToolCall{Name: hostPassesPaymentsQueue}
	record, err := entry.Prepare(ctx, "owner", 123, call, agent.Input{})
	require.NoError(t, err)
	result, err := entry.Execute(ctx, "owner", call, record, &agent.Input{})
	require.ErrorIs(t, err, denied)
	require.Nil(t, result)
	require.True(t, prepared)
	require.True(t, executed)
	require.Len(t, client.capabilityOwners, 5)
	require.Empty(t, client.eventOwners)
}

func TestPrivilegedAdmissionBindsOnlyFirstCurrentEvent(t *testing.T) {
	t.Parallel()
	first := core.PrivilegedReadEvent{Event: "historical-first",
		PaymentReads: true, PractitionerReads: true}
	client := &privilegedReadClient{page: core.ReadPage[core.PrivilegedReadEvent]{Items: []core.PrivilegedReadEvent{
		first, {Event: "second", PaymentReads: true},
	}}}
	record := ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: hostPrivilegesEvents},
		PrivilegedRead: &ScriptPrivilegedRead{Owner: "owner"}}
	bound, err := (PrivilegedReadHost{Client: client}).AdmitEventList(t.Context(), "owner", record)
	require.NoError(t, err)
	require.Equal(t, PrivilegedEventAuthorities("owner", []core.PrivilegedReadEvent{first}),
		bound.PrivilegedRead.Admission)
	require.Len(t, bound.PrivilegedRead.Admission, 2)
	require.Empty(t, bound.PrivilegedRead.Event)
	require.Equal(t, []string{"owner"}, client.eventOwners)
	require.Equal(t, []string{""}, client.cursors)
	require.Empty(t, client.capabilityOwners)
}

func TestPrivilegedFailuresDoNotBindPartialResults(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded,
		core.DatabaseFailure(&core.ProblemError{Status: 403, Code: "forbidden"}), errors.New("unavailable"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			client := &privilegedReadClient{failure: failure,
				roles: core.PrivilegedReadCapabilities{PaymentReads: true},
				page: core.ReadPage[core.PrivilegedReadEvent]{Items: []core.PrivilegedReadEvent{{Event: "partial",
					PaymentReads: true}}}}
			host := PrivilegedReadHost{Client: client}
			entries, err := host.Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, entries)
			record := ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: hostPrivilegesEvents},
				PrivilegedRead: &ScriptPrivilegedRead{Owner: "owner"}}
			bound, err := host.AdmitEventList(t.Context(), "owner", record)
			require.ErrorIs(t, err, failure)
			require.Empty(t, bound.PrivilegedRead.Admission)
			require.Equal(t, []string{"owner"}, client.eventOwners)
			require.Equal(t, []string{"owner"}, client.capabilityOwners)
		})
	}
}

func TestPrivilegedAdmissionRejectsMissingWitnessAndWrongBinding(t *testing.T) {
	t.Parallel()
	for _, items := range [][]core.PrivilegedReadEvent{nil, {{Event: "no-role"}}} {
		t.Run("witness", func(t *testing.T) {
			t.Parallel()
			client := &privilegedReadClient{page: core.ReadPage[core.PrivilegedReadEvent]{Items: items}}
			record := ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: hostPrivilegesEvents},
				PrivilegedRead: &ScriptPrivilegedRead{Owner: "owner"}}
			bound, err := (PrivilegedReadHost{Client: client}).AdmitEventList(t.Context(), "owner", record)
			require.ErrorContains(t, err, "privileged read admission missing")
			require.Empty(t, bound.PrivilegedRead.Admission)
			require.Len(t, client.eventOwners, 1)
		})
	}
	for _, record := range []ScriptToolRecord{
		{Outcome: agent.ScriptToolResult{Name: hostPrivilegesEvents}},
		{Outcome: agent.ScriptToolResult{Name: hostPrivilegesEvents}, PrivilegedRead: &ScriptPrivilegedRead{Owner: "other"}},
		{Outcome: agent.ScriptToolResult{Name: hostPassesPaymentsQueue}, PrivilegedRead: &ScriptPrivilegedRead{Owner: "owner"}},
		{Outcome: agent.ScriptToolResult{Name: hostPrivilegesEvents},
			PrivilegedRead: &ScriptPrivilegedRead{Owner: "owner", Event: "selected"}},
	} {
		t.Run("binding", func(t *testing.T) {
			t.Parallel()
			client := &privilegedReadClient{}
			_, err := (PrivilegedReadHost{Client: client}).AdmitEventList(t.Context(), "owner", record)
			require.ErrorContains(t, err, "privileged read admission missing")
			require.Empty(t, client.eventOwners)
		})
	}
}

func assertPrivilegedSchema(t *testing.T, descriptor scriptclient.Tool) {
	t.Helper()
	var schema struct {
		Properties map[string]struct {
			Type      string `json:"type"`
			MaxLength int    `json:"maxLength"`
		} `json:"properties"`
		Required             []string `json:"required"`
		AdditionalProperties bool     `json:"additionalProperties"`
	}
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
	require.False(t, schema.AdditionalProperties)
	if descriptor.Name == hostPrivilegesEvents {
		require.Empty(t, schema.Required)
		require.Len(t, schema.Properties, 1)
	} else {
		require.Equal(t, []string{"event"}, schema.Required)
		require.Equal(t, "string", schema.Properties["event"].Type)
		require.Equal(t, 200, schema.Properties["event"].MaxLength)
	}
	if descriptor.Name != hostMassagePractitionerPreferences {
		require.Equal(t, 2048, schema.Properties["cursor"].MaxLength)
	} else {
		require.Len(t, schema.Properties, 1)
	}
	if descriptor.Name == hostMassagePractitionerBookings {
		require.Equal(t, 200, schema.Properties["party"].MaxLength)
		require.Len(t, schema.Properties, 3)
	}
	for _, private := range []string{"owner", "version", "proof", "source", "operation_key"} {
		require.NotContains(t, schema.Properties, private)
	}
}
