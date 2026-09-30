package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"

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
			beforeEffects := privilegedEffectDigest(t, f)
			workerRuns := 0
			model := runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						workerRuns++
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
			require.Len(t, model.inputs, 1, "revoked evidence must not reach another model request")
			require.Equal(t, 1, workerRuns)
			assertPrivilegedRetirement(t, f, language)
			_, restoreErr := f.db.Exec(
				t.Context(),
				`INSERT INTO core.pass_payment_admins(event_id,owner,hidden) VALUES('script-dance','alice',true)`,
			)
			require.NoError(t, restoreErr)
			handle(t, f.b, message(1989, identity.AliceTelegramID, "Read my saved history and orders"))
			require.Len(t, model.inputs, 1, "the saved notice must prevent replanning after rights restoration")
			require.Equal(t, 1, workerRuns, "same-update replay must not rerun the worker")
			require.Equal(t, beforeEffects, privilegedEffectDigest(t, f))
			assertPrivilegedRetirement(t, f, language)
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

func privilegedEffectDigest(t *testing.T, f *fixture) string {
	t.Helper()
	var digest string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT md5(concat(
 (SELECT COALESCE(string_agg(row_to_json(r)::text,E'\n' ORDER BY row_to_json(r)::text),'') FROM core.pass_bookings r),
 (SELECT COALESCE(string_agg(row_to_json(r)::text,E'\n' ORDER BY row_to_json(r)::text),'') FROM core.pass_payment_attempts r),
 (SELECT COALESCE(string_agg(row_to_json(r)::text,E'\n' ORDER BY row_to_json(r)::text),'') FROM core.massage_bookings r)))`).Scan(&digest))
	return digest
}

func assertPrivilegedRetirement(t *testing.T, f *fixture, language string) {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=1989 AND kind='script_runs'`).
			Scan(&records),
	)
	require.Len(t, records, 1)
	require.True(t, records[0].PassRedacted)
	require.Empty(t, records[0].Request.Code)
	require.Empty(t, records[0].Request.InputJSON)
	var result struct {
		Omitted bool   `json:"omitted"`
		Reason  string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(records[0].Run.Result, &result))
	require.True(t, result.Omitted)
	require.Equal(t, "pass_access_changed", result.Reason)
	require.NotEmpty(t, records[0].Calls)
	for _, call := range records[0].Calls {
		require.Empty(t, call.Outcome.Result)
	}
	var kind, state string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind,state FROM interaction.saved_turns WHERE owner='alice' AND update_id=1989`).
			Scan(&kind, &state),
	)
	require.Equal(t, "notice", kind)
	require.Equal(t, "ready", state)
	pumpBotDeliveries(t, f.b)
	var wire struct {
		Messages []telegram.Message `json:"Messages"`
	}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT data FROM bot.fake_state WHERE id=true`).Scan(&wire))
	require.NotEmpty(t, wire.Messages)
	notice, err := i18n.Translate(language, i18n.AgentUnavailable, nil)
	require.NoError(t, err)
	require.Contains(t, strings.ReplaceAll(wire.Messages[len(wire.Messages)-1].Text, `\`, ""), notice)
	for _, message := range wire.Messages {
		require.NotContains(t, message.Text, "privileged-1")
		require.NotContains(t, message.Text, "visitor")
	}
}

// Definitive source revocation ends the admitted turn. A new input gets fresh
// context; it must not inherit the retired run or its transformed private result.
func assertPrivilegedFreshInput(t *testing.T, f *fixture, model *knowledgeModel, marker string) {
	t.Helper()
	require.NotEmpty(t, marker)
	require.Len(t, model.inputs, 1, "definitive revocation must end the admitted model turn")
	assertPrivilegedRetirement(t, f, model.inputs[0].Language)
	var before []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=1989 AND kind='script_runs'`).Scan(&before))
	require.NotContains(t, string(before), marker)
	effects := privilegedEffectDigest(t, f)
	handleVisible(t, f.b, message(1989, identity.AliceTelegramID, "Read my saved history and orders"))
	require.Len(t, model.inputs, 1, "replaying the terminal update must not replan")
	assertPrivilegedRetirement(t, f, model.inputs[0].Language)
	handleVisible(t, f.b, message(1990, identity.AliceTelegramID, "Read my current available information"))
	require.Len(t, model.inputs, 2, "an independent fresh input must remain usable")
	require.NotNil(t, model.inputs[1].Script)
	require.EqualValues(t, 1990, model.inputs[1].Script.UpdateID)
	require.Empty(t, model.inputs[1].Script.Runs, "the new input cannot resume the retired VM")
	require.Empty(t, model.inputs[1].Script.ReadAuthorities)
	fresh, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	require.NotContains(t, string(fresh), marker)
	var after []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=1989 AND kind='script_runs'`).Scan(&after))
	require.JSONEq(t, string(before), string(after), "replay and fresh input cannot resurrect retired source evidence")
	require.Equal(t, effects, privilegedEffectDigest(t, f))
}
