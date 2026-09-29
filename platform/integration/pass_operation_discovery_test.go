package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func saveOperationReference(
	t *testing.T,
	service derivedmutation.Service,
	command passbooking.RuntimeBatch,
	source readsource.Derivation,
) string {
	t.Helper()
	id := rand.Text()
	content, err := json.Marshal([]any{map[string]any{"calls": []any{map[string]any{
		"pass": map[string]any{"id": id, "name": "passes.batch.assign", "batch": command}, "source": source,
		"outcome": map[string]any{"result": map[string]any{"private": "NEVER-RETURN-SCRIPT-BODY"}},
	}}}})
	require.NoError(t, err)
	_, err = service.DB.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('visitor',$1,'script_runs',$2)`,
		int64(len(command.Key)),
		content,
	)
	require.NoError(t, err)
	return id
}

func TestPassOperationDiscoveryReceiptContinuity(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"resume", "history", "external_batch", "grant", "target_grant", "missing_receipt"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			_, service, command, source := successorBatchFixture(t)
			require.NoError(
				t,
				(conversation.Service{DB: service.DB}).AppendOriginal(
					t.Context(),
					"visitor",
					"successor-original",
					"user",
					"PRIVATE-OPERATION-SOURCE",
				),
			)
			interruptSuccessorBatch(t, service, command, source)
			id := saveOperationReference(t, service, command, source)
			query := derivedmutation.PassOperationQuery{ID: id}
			before, err := operationSummaries(t.Context(), service, "visitor", query)
			require.NoError(t, err)
			require.Len(t, before, 1)
			require.Equal(t, "pending", before[0].Status)
			require.Equal(t, 1, before[0].Committed)
			require.Equal(t, 1, before[0].Pending)
			require.NotNil(t, before[0].Context, "same-batch committed successor keeps authorized context")
			changeSuccessorState(t, service, command, scenario)
			after, err := operationSummaries(t.Context(), service, "visitor", query)
			switch scenario {
			case "grant", "target_grant":
				requireCode(t, err, "pass_operation_unavailable")
				list, listErr := operationSummaries(
					t.Context(),
					service,
					"visitor",
					derivedmutation.PassOperationQuery{},
				)
				require.NoError(t, listErr)
				require.Empty(t, list)
			case "missing_receipt":
				require.Error(t, err)
			default:
				require.NoError(t, err)
				require.Equal(t, 1, after[0].Committed)
				if scenario != "resume" {
					require.Nil(t, after[0].Context)
					require.Equal(t, "unavailable", after[0].Continuation)
				}
				raw, encodeErr := json.Marshal(after)
				require.NoError(t, encodeErr)
				require.NotContains(t, string(raw), "PRIVATE-OPERATION-SOURCE")
				require.NotContains(t, string(raw), "NEVER-RETURN-SCRIPT-BODY")
			}
			if scenario == "resume" {
				_, err = service.RunManualPassBatch(t.Context(), "visitor", command)
				require.NoError(t, err)
				after, err = operationSummaries(t.Context(), service, "visitor", query)
				require.NoError(t, err)
				require.Equal(t, "complete", after[0].Status)
				require.Equal(t, 2, after[0].Committed)
			}
		})
	}
}

func TestPassOperationLocalHTTPParity(t *testing.T) {
	t.Parallel()
	db, service, command, source := successorBatchFixture(t)
	interruptSuccessorBatch(t, service, command, source)
	id := saveOperationReference(t, service, command, source)
	signer := identity.Signer{Key: []byte("operation-discovery-test-key-32-bytes")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.Services{
				Core:             core.Service{DB: db},
				Registration:     service.Registration,
				DerivedMutations: service,
			},
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	userToken := func(_ context.Context, owner string) (string, error) { return signer.Token(owner), nil }
	local := appclient.Host{
		UserToken: userToken,
		LocalDerived: &appclient.LocalDerived{
			Service:    service,
			Authorizer: applicationauth.Authorizer{DB: db, Verify: verify},
		},
	}
	remote := appclient.Host{Base: server.URL, HTTP: server.Client(), Signer: signer, UserToken: userToken}
	query := derivedmutation.PassOperationQuery{ID: id}
	localRead := interaction.RegistrationOperations{Ledger: agenthost.ScriptStore{DB: db}, Domain: local}
	remoteRead := interaction.RegistrationOperations{Ledger: agenthost.ScriptStore{DB: db}, Domain: remote}
	want, err := localRead.Read(t.Context(), "visitor", query)
	require.NoError(t, err)
	got, err := remoteRead.Read(t.Context(), "visitor", query)
	require.NoError(t, err)
	require.JSONEq(t, string(operationJSON(t, want)), string(operationJSON(t, got)))
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL+"/internal/derived/pass-operation",
		bytes.NewReader(operationJSON(t, derivedmutation.PassOperationInput{Batch: &command, Source: &source})),
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+signer.Token("visitor"))
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(
		t,
		http.StatusUnauthorized,
		response.StatusCode,
		"ordinary user identity cannot impersonate the host read boundary",
	)
	for _, host := range []interaction.RegistrationOperations{localRead, remoteRead} {
		_, err = host.Read(t.Context(), "alice", query)
		requireCode(t, err, "pass_operation_unavailable")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = host.Read(ctx, "visitor", query)
		require.ErrorIs(t, err, context.Canceled)
	}
}

func operationJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestPassOperationSelection(t *testing.T) {
	t.Parallel()
	_, service, first, source := successorBatchFixture(t)
	interruptSuccessorBatch(t, service, first, source)
	firstID := saveOperationReference(t, service, first, source)
	second := first
	second.Key = "other-operation-not-executed"
	second.Recipients = []int64{202}
	secondID := saveOperationReference(t, service, second, source)
	list, err := operationSummaries(t.Context(), service, "visitor", derivedmutation.PassOperationQuery{})
	require.NoError(t, err)
	require.Len(t, list, 2)
	var selected string
	for _, item := range list {
		require.Nil(t, item.Items)
		if item.Committed == 1 && item.Pending == 1 {
			selected = item.ID
		}
	}
	require.Equal(t, firstID, selected)
	require.NotEqual(t, secondID, selected)
	_, err = service.RunManualPassBatch(t.Context(), "visitor", first)
	require.NoError(t, err)
	other, err := operationSummaries(t.Context(), service, "visitor", derivedmutation.PassOperationQuery{ID: secondID})
	require.NoError(t, err)
	require.Equal(t, "not_committed", other[0].Status)
	var batches int
	require.NoError(t, service.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_batches`).Scan(&batches))
	require.Equal(t, 1, batches)
}

func TestPassOperationPublicToolSelection(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Synthetic Name'`)
	require.NoError(t, err)
	for i, script := range []string{
		`return tools.passes.batch.assign({event:"dance",recipients:[101],assignment:{create:true,from_profile:true}});`,
		`return tools.passes.batch.assign({event:"dance",recipients:[202],assignment:{create:true,from_profile:true}});`,
	} {
		f.b.Host.HTTP = &http.Client{Transport: &passLostReply{}}
		result := runPassVM(
			t,
			f,
			65000+int64(i),
			202,
			fmt.Sprintf("Assign Telegram ID %d to dance from their profile", 101*(i+1)),
			script,
		)
		t.Logf("admission %d error=%s calls=%+v", i, result.Error, result.Calls)
		require.Empty(t, result.Error)
		require.Contains(t, string(result.Result), "operation_id")
	}
	f.b.Host.HTTP = f.b.API.HTTP
	service := passbooking.Service{DB: f.db}
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	result := runPassVM(t, f, 65002, 202, "Resume the prior operation for recipient 101 only", `
const operations = await tools.passes.operations({});
const selected = operations.find(o => o.context && o.context.recipients.includes(101));
if (!selected) throw new Error("operation not discoverable");
return {selected, resumed: await tools.passes.resume({operation_id:selected.operation_id})};`)
	require.Empty(t, result.Error)
	var response struct {
		Selected interaction.RegistrationOperationSummary `json:"selected"`
		Resumed  struct {
			ID       string `json:"operation_id"`
			Complete bool   `json:"complete"`
		} `json:"resumed"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &response))
	require.Equal(t, []int64{101}, response.Selected.Context.Recipients)
	require.Equal(t, response.Selected.ID, response.Resumed.ID)
	require.True(t, response.Resumed.Complete)
	afterAlice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	afterBob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, alice, afterAlice)
	require.Equal(t, bob, afterBob)
}

func TestPassOperationReceiptFamilies(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"command", "assignment", "cancel", "uncouple"} {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			db, registration := adminPairFixture(t)
			service := derivedmutation.Service{DB: db, Registration: registration}
			generation := int64(0)
			source := readsource.Derivation{
				Generation:     &generation,
				PrivateHistory: true,
				Authorities:    []readsource.Authority{},
			}
			id := rand.Text()
			request := map[string]any{"id": id}
			switch family {
			case "command":
				command := passbooking.Command{
					Name:          "admin_cancel",
					Event:         "dance",
					Key:           "single-cancel",
					Version:       1,
					Target:        "alice",
					TargetVersion: 1,
				}
				_, err := service.ExecutePassBooking(t.Context(), "bob", command, source)
				require.NoError(t, err)
				request["name"], request["command"] = "passes.admin.cancel", command
			case "assignment":
				price := 987
				comment := "PRIVATE-ASSIGNMENT-COMMENT"
				command := passbooking.AdminAssignment{
					Event:         "dance",
					Key:           "single-assignment",
					Version:       1,
					Target:        "alice",
					TargetVersion: 1,
					TotalPrice:    &price,
					Comment:       &comment,
				}
				_, err := service.AssignPass(t.Context(), "bob", command, source)
				require.NoError(t, err)
				request["name"], request["assignment"] = "passes.admin.assign", command
			default:
				action := "admin_cancel"
				if family == "uncouple" {
					action = "admin_uncouple"
				}
				command := passbooking.RuntimeBatch{
					Event:      "dance",
					Key:        "batch-" + family,
					Action:     action,
					Recipients: []int64{101},
				}
				items, err := service.RunPassBatch(t.Context(), "bob", command, source)
				require.NoError(t, err)
				require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
				request["name"], request["batch"] = "passes.batch."+family, command
			}
			content := operationJSON(
				t,
				[]any{map[string]any{"calls": []any{map[string]any{"pass": request, "source": source}}}},
			)
			_, err := db.Exec(
				t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('bob',70000,'script_runs',$1)`,
				content,
			)
			require.NoError(t, err)
			status, err := operationSummaries(t.Context(), service, "bob", derivedmutation.PassOperationQuery{ID: id})
			require.NoError(t, err)
			require.Len(t, status, 1)
			require.NotContains(t, string(operationJSON(t, status)), "PRIVATE-ASSIGNMENT-COMMENT")
			if family == "command" || family == "assignment" {
				require.Equal(t, "committed", status[0].Status)
			} else {
				require.Equal(t, "complete", status[0].Status)
				require.Equal(t, 1, status[0].Committed)
			}
		})
	}
}

// The test reads the real host ledger and composes current domain evidence.
func operationSummaries(ctx context.Context, service derivedmutation.Service, owner string,
	query derivedmutation.PassOperationQuery) ([]interaction.RegistrationOperationSummary, error) {
	result, err := (interaction.RegistrationOperations{
		Ledger: agenthost.ScriptStore{DB: service.DB}, Domain: service,
	}).Read(ctx, owner, query)
	return result.Summaries, err
}
