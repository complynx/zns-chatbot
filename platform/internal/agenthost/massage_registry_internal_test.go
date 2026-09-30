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

type massageCatalogCapabilities struct {
	current core.PrivilegedReadCapabilities
	failure error
	owners  []string
}

func (c *massageCatalogCapabilities) PrivilegedReadCapabilities(
	_ context.Context, owner string,
) (core.PrivilegedReadCapabilities, error) {
	c.owners = append(c.owners, owner)
	return c.current, c.failure
}

type liveMassageCatalog struct {
	liveScriptCatalog

	massage MassageScriptCatalog
}

func (c *liveMassageCatalog) Massage(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	return c.massage.Entries(ctx, owner)
}

func TestMassageCatalogRoleSchemaAndBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		role core.PrivilegedReadCapabilities
	}{
		{name: "ordinary"},
		{name: "unrelated-payment-role", role: core.PrivilegedReadCapabilities{PaymentReads: true}},
		{name: "practitioner", role: core.PrivilegedReadCapabilities{PractitionerReads: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &massageCatalogCapabilities{current: test.role}
			entries, err := (MassageScriptCatalog{Client: client, Binding: ScriptToolEntry{ResultLimit: 32 << 10}}).
				Entries(t.Context(), "owner")
			require.NoError(t, err)
			expected := []string{scriptMassageBook, scriptMassageCancel}
			if test.role.PractitionerReads {
				expected = append(expected, scriptMassageInstant, scriptMassageConfigure)
			}
			names := make([]string, 0, len(entries))
			required := map[string][]string{
				scriptMassageBook:      {"event", "party", "specialist", "start", "length"},
				scriptMassageCancel:    {"event", "booking"},
				scriptMassageInstant:   {"event", "party", "length"},
				scriptMassageConfigure: {"event", "bookings", "next"},
			}
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 32<<10, entry.ResultLimit)
				var schema struct {
					Type                 string                     `json:"type"`
					AdditionalProperties bool                       `json:"additionalProperties"`
					Properties           map[string]json.RawMessage `json:"properties"`
					Required             []string                   `json:"required"`
				}
				require.NoError(t, json.Unmarshal(entry.Descriptor.InputSchema, &schema))
				require.Equal(t, "object", schema.Type)
				require.False(t, schema.AdditionalProperties)
				require.Equal(t, required[entry.Descriptor.Name], schema.Required)
				for _, forbidden := range []string{"owner", "key", "source", "version", "slot", "telegram_id"} {
					require.NotContains(t, schema.Properties, forbidden)
				}
				switch entry.Descriptor.Name {
				case scriptMassageBook:
					var length struct {
						Enum []int `json:"enum"`
					}
					require.NoError(t, json.Unmarshal(schema.Properties["length"], &length))
					require.Equal(t, []int{1, 2, 3, 5}, length.Enum)
					var start struct {
						Format string `json:"format"`
					}
					require.NoError(t, json.Unmarshal(schema.Properties["start"], &start))
					require.Equal(t, "date-time", start.Format)
				case scriptMassageInstant:
					var length struct {
						Type    string `json:"type"`
						Minimum int    `json:"minimum"`
						Maximum int    `json:"maximum"`
					}
					require.NoError(t, json.Unmarshal(schema.Properties["length"], &length))
					require.Equal(t, "integer", length.Type)
					require.Equal(t, 1, length.Minimum)
					require.Equal(t, 6, length.Maximum)
				case scriptMassageConfigure:
					for _, flag := range []string{"bookings", "next"} {
						var field struct {
							Type string `json:"type"`
						}
						require.NoError(t, json.Unmarshal(schema.Properties[flag], &field))
						require.Equal(t, "boolean", field.Type)
					}
				}
			}
			require.Equal(t, expected, names)
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}

func TestMassageCatalogLiveDiscoveryScopeAndEventDenial(t *testing.T) {
	t.Parallel()
	client := &massageCatalogCapabilities{}
	prepared, executed := false, false
	denied := &core.ProblemError{Status: 403, Code: "event_practitioner_forbidden"}
	binding := ScriptToolEntry{ResultLimit: 32 << 10,
		Prepare: func(_ context.Context, owner string, update int64, call scriptclient.ToolCall,
			_ agent.Input,
		) (ScriptToolRecord, error) {
			require.Equal(t, "owner", owner)
			require.Equal(t, int64(123), update)
			require.Equal(t, scriptMassageInstant, call.Name)
			prepared = true
			return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
		},
		Execute: func(_ context.Context, owner string, call scriptclient.ToolCall, record ScriptToolRecord,
			_ *agent.Input,
		) (any, error) {
			require.Equal(t, "owner", owner)
			require.Equal(t, call.Name, record.Outcome.Name)
			executed = true
			return nil, denied
		},
	}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: &liveMassageCatalog{
		massage: MassageScriptCatalog{Client: client, Binding: binding},
	}}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	client.current.PractitionerReads = true
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Contains(t, string(list), scriptMassageBook)
	require.NotContains(t, string(list), scriptMassageInstant)
	_, err = host.Registry.Resolve(ctx, "owner", scriptMassageInstant)
	require.Error(t, err, "new grant cannot broaden this VM")
	require.Len(t, client.owners, 2)
	fresh, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	freshCtx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(fresh))
	client.current.PractitionerReads = false
	_, err = host.discover(freshCtx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"massage.practitioner.instant"}`),
	})
	require.Error(t, err)
	_, err = host.Registry.Resolve(freshCtx, "owner", scriptMassageInstant)
	require.Error(t, err, "known-name call cannot bypass revocation")
	require.False(t, prepared)
	require.False(t, executed)
	require.Len(t, client.owners, 5)
	client.current.PractitionerReads = true
	help, err := host.discover(freshCtx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"massage.practitioner.instant"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "current practitioner of that event")
	entry, err := host.Registry.Resolve(freshCtx, "owner", scriptMassageInstant)
	require.NoError(t, err)
	call := scriptclient.ToolCall{Name: scriptMassageInstant}
	record, err := entry.Prepare(freshCtx, "owner", 123, call, agent.Input{})
	require.NoError(t, err)
	result, err := entry.Execute(freshCtx, "owner", call, record, &agent.Input{})
	require.ErrorIs(t, err, denied)
	require.Nil(t, result)
	require.True(t, prepared)
	require.True(t, executed)
	require.Equal(t, 32<<10, entry.ResultLimit)
}

func TestMassageCatalogCapabilityFailureNeverYieldsPartialTools(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled,
		core.DatabaseFailure(&core.ProblemError{Status: 403, Code: "forbidden"}), errors.New("unavailable"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			client := &massageCatalogCapabilities{
				current: core.PrivilegedReadCapabilities{PractitionerReads: true}, failure: failure,
			}
			entries, err := (MassageScriptCatalog{Client: client}).Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, entries)
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}
