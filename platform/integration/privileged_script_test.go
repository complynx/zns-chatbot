package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func privilegedToolNames() []string {
	return []string{
		"privileges.events",
		"passes.payments.queue",
		"passes.payments.history",
		"massage.practitioner.schedule",
		"massage.practitioner.preferences",
		"massage.practitioner.bookings",
	}
}

func TestPrivilegedScriptNamesHiddenWithoutRoles(t *testing.T) {
	t.Parallel()
	f := setup(t)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				names, err := json.Marshal(tools)
				require.NoError(t, err)
				list := scriptCall(ctx, t, callback, "$list", "{}")
				for _, name := range privilegedToolNames() {
					assert.NotContains(t, string(names), name)
					assert.NotContains(t, string(list), name)
					helpArgs, encodeErr := json.Marshal(map[string]string{"name": name})
					require.NoError(t, encodeErr)
					_, callErr := callback(ctx, scriptclient.ToolCall{Name: "$help", Arguments: helpArgs})
					require.Error(t, callErr)
					_, callErr = callback(
						ctx,
						scriptclient.ToolCall{Name: name, Arguments: json.RawMessage(`{"event":"sandbox-festival"}`)},
					)
					require.Error(t, callErr)
				}
				return json.RawMessage(`{"hidden":true}`), nil
			},
		),
	)
}

func TestPrivilegedScriptLiveReadsAndScopeRevocation(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			seedPrivilegedReads(t, f)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			model := runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						bindings, encodeErr := json.Marshal(tools)
						require.NoError(t, encodeErr)
						for _, name := range privilegedToolNames() {
							assert.Contains(t, string(bindings), name)
						}
						list := scriptCall(ctx, t, callback, "$list", "{}")
						assert.Contains(t, string(list), "passes.payments.history")
						help := scriptCall(ctx, t, callback, "$help", `{"name":"massage.practitioner.preferences"}`)
						assert.NotContains(t, string(help), "owner")
						scopes := scriptCall(ctx, t, callback, "privileges.events", `{}`)
						assert.Contains(t, string(scopes), "script-dance")
						assert.Contains(t, string(scopes), "sandbox-festival")
						var logs bytes.Buffer
						recorder, recordErr := observability.NewAgentEvents(
							slog.New(slog.NewJSONHandler(&logs, nil)),
							"00000000-0000-4000-8000-000000000001",
							1,
						)
						require.NoError(t, recordErr)
						history := scriptCall(
							observability.WithAgentEvents(ctx, recorder),
							t,
							callback,
							"passes.payments.history",
							`{"event":"script-dance"}`,
						)
						assert.Contains(t, logs.String(), "passes.payments.history")
						assert.NotContains(t, logs.String(), "script-dance")
						assert.NotContains(t, logs.String(), "visitor")
						assert.NotContains(t, logs.String(), "receiving_admin")
						assert.Contains(t, string(history), "visitor")
						scriptCall(ctx, t, callback, "passes.payments.queue", `{"event":"script-dance"}`)
						schedule := scriptCall(
							ctx,
							t,
							callback,
							"massage.practitioner.schedule",
							`{"event":"sandbox-festival"}`,
						)
						assert.Contains(t, string(schedule), "next_cursor")
						prefs := scriptCall(
							ctx,
							t,
							callback,
							"massage.practitioner.preferences",
							`{"event":"sandbox-festival"}`,
						)
						assert.Contains(t, string(prefs), "bookings")
						assigned := scriptCall(
							ctx,
							t,
							callback,
							"massage.practitioner.bookings",
							`{"event":"sandbox-festival"}`,
						)
						assert.Contains(t, string(assigned), "privileged-")
						assert.NotContains(t, string(assigned), "foreign-massage")
						_, callErr := callback(
							ctx,
							scriptclient.ToolCall{
								Name:      "passes.payments.history",
								Arguments: json.RawMessage(`{"event":"sandbox-festival"}`),
							},
						)
						require.Error(t, callErr)
						_, deleteErr := f.db.Exec(ctx, `DELETE FROM core.pass_payment_admins WHERE owner='alice'`)
						require.NoError(t, deleteErr)
						after := scriptCall(ctx, t, callback, "$list", "{}")
						assert.NotContains(t, string(after), "passes.payments.")
						assert.Contains(t, string(after), "massage.practitioner.")
						_, callErr = callback(
							ctx,
							scriptclient.ToolCall{
								Name:      "passes.payments.history",
								Arguments: json.RawMessage(`{"event":"script-dance"}`),
							},
						)
						require.Error(t, callErr)
						return json.RawMessage(`{"read_complete":true}`), nil
					},
				),
			)
			require.Len(t, model.inputs, 2)
			encoded, err := json.Marshal(model.inputs[1].Script)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "privileged-1")
			assert.NotContains(t, string(encoded), "visitor")
			assert.Contains(t, string(encoded), "payload_omitted")
		})
	}
}

func TestPrivilegedScriptRoleRevokedBetweenDiscoveryAndCall(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_events(id) VALUES('role-only'); INSERT INTO core.massage_specialists(event_id,owner,name) VALUES('role-only','alice','Me')`,
	)
	require.NoError(t, err)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				before := scriptCall(ctx, t, callback, "$list", "{}")
				assert.Contains(t, string(before), "massage.practitioner.preferences")
				_, deleteErr := f.db.Exec(
					ctx,
					`DELETE FROM core.massage_specialists WHERE event_id='role-only' AND owner='alice'`,
				)
				require.NoError(t, deleteErr)
				_, callErr := callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "massage.practitioner.preferences",
						Arguments: json.RawMessage(`{"event":"role-only"}`),
					},
				)
				require.Error(t, callErr)
				after := scriptCall(ctx, t, callback, "$list", "{}")
				for _, name := range privilegedToolNames() {
					assert.NotContains(t, string(after), name)
				}
				caps, readErr := (core.Service{DB: f.db}).PrivilegedReads(ctx, "alice")
				require.NoError(t, readErr)
				assert.False(t, caps.PractitionerReads)
				return json.RawMessage(`{"revoked":true}`), nil
			},
		),
	)
}

func TestPrivilegedScriptPaymentQueueContinuesAcrossCorePages(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('script-dance','alice');
 INSERT INTO core.users(id,telegram_id,name,can_book) SELECT 'queue-payer-'||n,5000+n,'Synthetic',true FROM generate_series(1,41)n;
 INSERT INTO core.order_proofs(id,owner,filename,body) SELECT md5(id)||md5(id),id,'hidden.txt','test' FROM core.users WHERE id LIKE 'queue-payer-%';
 INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at) SELECT md5(owner)||md5(owner),'script-dance',owner,id,'bob',clock_timestamp() FROM core.order_proofs;
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,payment_attempt)
 SELECT 'script-dance',submitter,1,'paid','leader','solo','bob',received_at,received_at,100,id FROM core.pass_payment_attempts`,
	)
	require.NoError(t, err)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				cursor := ""
				seen := map[string]bool{}
				for {
					args, encodeErr := json.Marshal(map[string]string{"event": "script-dance", "cursor": cursor})
					require.NoError(t, encodeErr)
					data := scriptCall(ctx, t, callback, "passes.payments.queue", string(args))
					var page struct {
						Items []struct {
							Payment struct {
								Attempt string `json:"attempt"`
							} `json:"payment"`
						} `json:"items"`
						Next string `json:"next_cursor"`
						More bool   `json:"more"`
					}
					require.NoError(t, json.Unmarshal(data, &page))
					require.LessOrEqual(t, len(page.Items), 20)
					for _, item := range page.Items {
						assert.False(t, seen[item.Payment.Attempt])
						seen[item.Payment.Attempt] = true
					}
					if !page.More {
						break
					}
					require.NotEmpty(t, page.Next)
					cursor = page.Next
				}
				assert.Len(t, seen, 41)
				return json.RawMessage(`{"count":41}`), nil
			},
		),
	)
}
