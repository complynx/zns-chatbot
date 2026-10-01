package integration_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type passDeliveryTransport func(*http.Request) (*http.Response, error)

func (f passDeliveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func runPassDeliveryVM(t *testing.T, f *fixture, update, user int64, code string) error {
	t.Helper()
	f.b.Scripts = scopeVM{}
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: "workflow", Text: "Checked"},
	}}
	return f.b.Handle(t.Context(), message(update, user, "Perform pass operation"))
}

func deletePassDeliveryHistory(t *testing.T, f *fixture, owner string) {
	t.Helper()
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner=$1 AND origin='original' AND omission_reason<>'deleted' ORDER BY id LIMIT 1`, owner).
			Scan(&id),
	)
	require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), owner, id))
}

func TestDerivedPassMenuRetainsSourceThroughRefreshAndFallback(t *testing.T) {
	t.Parallel()
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback_%t", fallback), func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			run := runPassVM(
				t,
				f,
				72000,
				101,
				"Show my pass",
				`return tools.passes.registration.show({event:"dance",view:"home"});`,
			)
			require.Empty(t, run.Error)
			var bound bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state->'source' IS NOT NULL FROM bot.pass_views WHERE owner='alice'`).
					Scan(&bound),
			)
			require.True(t, bound)
			pumpBotDeliveries(t, f.b)
			before := chatMessages(t, f, 101)
			beforeCard := passMenuCard(t, f, 101)
			var originalSource string
			require.NoError(t, f.db.QueryRow(t.Context(),
				`SELECT (state->'source')::text FROM bot.pass_views WHERE owner='alice'`).Scan(&originalSource))
			failed := false
			if fallback {
				f.b.TG.HTTP = &http.Client{
					Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
						if !failed && strings.HasSuffix(request.URL.Path, "/editMessageText") {
							failed = true
							deletePassDeliveryHistory(t, f, "alice")
							return &http.Response{
								StatusCode: http.StatusBadRequest,
								Header:     http.Header{},
								Body: io.NopCloser(
									strings.NewReader(
										`{"ok":false,"error_code":400,"description":"message to edit not found"}`,
									),
								),
								Request: request,
							}, nil
						}
						return http.DefaultTransport.RoundTrip(request)
					}),
				}
			} else {
				deletePassDeliveryHistory(t, f, "alice")
			}
			require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
			pumpBotDeliveries(t, f.b)
			if fallback {
				require.True(t, failed, "fallback must reach the injected edit failure")
				// The missing-edit response defers the send phase without a provider cooldown.
				pumpBotDeliveries(t, f.b)
			}
			after := chatMessages(t, f, 101)
			require.Len(t, after, len(before), "revocation must not send a new card")
			card := passMenuCard(t, f, 101)
			require.Equal(t, beforeCard.ID, card.ID)
			require.Empty(t, card.Markup.Rows)
			require.Contains(t, card.Text, "/passes")
			var tombstone bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT (state->>'redacted')::boolean AND state->'source' IS NOT NULL FROM bot.pass_views WHERE owner='alice'`).
					Scan(&tombstone),
			)
			require.True(t, tombstone)
			var retiredSource string
			require.NoError(t, f.db.QueryRow(t.Context(),
				`SELECT (state->'source')::text FROM bot.pass_views WHERE owner='alice'`).Scan(&retiredSource))
			require.Equal(t, originalSource, retiredSource)
			f.b.TG.HTTP = nil
			handle(t, f.b, message(72001, 101, "/passes"))
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state->'source' IS NOT NULL FROM bot.pass_views WHERE owner='alice'`).
					Scan(&bound),
			)
			require.False(t, bound, "explicit manual reset builds independent menu state")
		})
	}
}

func TestDerivedPassMenuRevocationBeforePersistence(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	require.NoError(
		t,
		(conversation.Service{DB: f.db}).AppendOriginal(
			t.Context(),
			"alice",
			"menu-source",
			"user",
			"Earlier menu context",
		),
	)
	hit := false
	f.b.API.HTTP = &http.Client{Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
		response, requestErr := http.DefaultTransport.RoundTrip(request)
		if requestErr != nil || hit || request.URL.Path != "/v1/passes/events/dance/me" {
			return response, requestErr
		}
		var admitted bool
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT EXISTS(
 SELECT 1 FROM bot.interactions i CROSS JOIN LATERAL jsonb_array_elements(i.content) run
 CROSS JOIN LATERAL jsonb_array_elements(run->'calls') call
 WHERE i.owner='alice' AND i.update_id=72300 AND i.kind='script_runs' AND call->'pass'->'menu' IS NOT NULL)`).Scan(&admitted))
		if admitted {
			hit = true
			deletePassDeliveryHistory(t, f, "alice")
		}
		return response, requestErr
	})}
	err := runPassDeliveryVM(t, f, 72300, 101, `return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Error(t, err, "history revocation terminates the admitted turn")
	require.True(t, hit, "revocation must follow admitted request and target authorization")
	var views int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_views WHERE owner='alice'`).Scan(&views),
	)
	require.Zero(t, views, "source rejection must roll back menu persistence")
}

func TestDerivedPassExportRejectsLateSourceAndPartialGrantRevocation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"history", "partial_grant"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			require.NoError(
				t,
				(conversation.Service{DB: f.db}).AppendOriginal(
					t.Context(),
					"bob",
					"export-source",
					"user",
					"Earlier export context",
				),
			)
			_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob';
 INSERT INTO core.pass_events(id,finishes_at) VALUES('other',now()+interval '30 days');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('other','bob')`)
			require.NoError(t, err)
			hit := false
			f.b.Host.HTTP = &http.Client{
				Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
					response, requestErr := http.DefaultTransport.RoundTrip(request)
					if requestErr == nil && !hit && response.StatusCode == http.StatusOK &&
						request.URL.Path == "/internal/passes/export-snapshot" {
						hit = true
						revokePassExportSource(t, f, kind)
					}
					return response, requestErr
				}),
			}
			wire := 0
			f.b.TG.HTTP = &http.Client{
				Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
					if strings.HasSuffix(request.URL.Path, "/sendDocument") {
						wire++
					}
					return http.DefaultTransport.RoundTrip(request)
				}),
			}
			require.NoError(t, runPassDeliveryVM(t, f, 72100, 202, "return tools.passes.export({});"))
			require.False(t, hit, "admission must not render the document")
			require.Zero(t, wire)
			operation, effect := botdelivery.ResultOperation("bob", 72100, "document:pass_export::")
			ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
			before, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
			require.NoError(t, readErr)
			require.Equal(t, delivery.Deferred, before.State)
			require.NotNil(t, before.Reference.Source)
			waitExportBoundaryCandidate(t, f, ref, time.Second)
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
			require.True(t, hit, "revoke after the worker obtained its authorized snapshot")
			after, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
			require.NoError(t, readErr)
			require.Equal(t, delivery.Cancelled, after.State)
			require.Equal(t, before.Reference, after.Reference)
			require.Zero(t, after.MessageID)
			require.Zero(t, wire)
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
			require.Zero(t, wire, "cancelled export must not replay")
			var receipts int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND kind='pass_export'`).
					Scan(&receipts),
			)
			require.Zero(t, receipts)
			for _, message := range chatMessages(t, f, 202) {
				require.Nil(t, message.Document)
			}
			if kind == "partial_grant" {
				capabilities, capabilityErr := (passbooking.Service{DB: f.db}).ToolCapabilities(t.Context(), "bob")
				require.NoError(t, capabilityErr)
				require.True(t, capabilities.Export, "unrelated export permission remains")
			}
		})
	}
}

func TestDerivedPassResumeCannotReplaceOriginalSource(t *testing.T) {
	t.Parallel()
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%t", legacy), func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			blocked := false
			f.b.Host.HTTP = &http.Client{
				Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
					if request.URL.Path == "/internal/derived/pass-actions" && !blocked {
						blocked = true
						return nil, errors.New("synthetic interruption before effect")
					}
					return http.DefaultTransport.RoundTrip(request)
				}),
			}
			run := runPassVM(
				t,
				f,
				72200,
				101,
				"Register solo",
				`await tools.passes.registration.read({event:"dance",view:"home"}); return tools.passes.registration.solo({event:"dance"});`,
			)
			require.True(
				t,
				blocked,
				"mutation must reach the injected transport barrier: %s / %s",
				run.Error,
				run.Result,
			)
			require.Empty(t, run.Error)
			var result struct {
				ID       string `json:"operation_id"`
				Complete bool   `json:"complete"`
			}
			require.NoError(t, json.Unmarshal(run.Result, &result))
			require.NotEmpty(t, result.ID)
			require.False(t, result.Complete)
			if legacy {
				_, err := f.db.Exec(
					t.Context(),
					`UPDATE bot.interactions SET content=jsonb_set(content,'{0,calls,1,source}','null') WHERE owner='alice' AND update_id=72200 AND kind='script_runs'`,
				)
				require.NoError(t, err)
			} else {
				deletePassDeliveryHistory(t, f, "alice")
			}
			resumed := runPassVM(
				t,
				f,
				72201,
				101,
				"Resume registration",
				fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, result.ID),
			)
			require.NotEmpty(t, resumed.Error)
			var effects int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).
					Scan(&effects),
			)
			require.Zero(t, effects)
		})
	}
}

func revokePassExportSource(t *testing.T, f *fixture, kind string) {
	t.Helper()
	if kind == "history" {
		deletePassDeliveryHistory(t, f, "bob")
		return
	}
	_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='bob'`)
	require.NoError(t, err)
}

func waitExportBoundaryCandidate(t *testing.T, f *fixture, ref delivery.Reference, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, entry := range botDeliveryCandidates(t, f.b) {
			if entry.Reference == ref {
				return true
			}
		}
		return false
	}, timeout, 50*time.Millisecond)
}

func TestDerivedPassMenuRetiresAfterRevocationDuringReconstruction(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"domain", "admission", "receipt", "admission_grant", "receipt_grant"} {
		t.Run(stage, func(t *testing.T) { t.Parallel(); runPassReconstructionBoundary(t, stage) })
	}
}

func TestDerivedPassMenuGenericStalePreservesAuthorizedView(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"empty_capture", "target_obsolete", "newer_revision"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			var err error
			run := runPassVM(t, f, 72500, 101, "Show my pass",
				`return tools.passes.registration.show({event:"dance",view:"home"});`)
			require.Empty(t, run.Error)
			pumpBotDeliveries(t, f.b)
			before := chatMessages(t, f, 101)
			beforeCard := passMenuCard(t, f, 101)
			var operation, effect string
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'family'='passes' AND state='sent'
 ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
			original, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID,
				delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}, false)
			require.NoError(t, err)
			reference := original.Reference
			if kind != "empty_capture" {
				reference.Notice = i18n.RegistrationSaved
			}
			require.NoError(t, f.b.Host.EnqueueBotCard(t.Context(), botdelivery.CardRequest{
				Owner: "alice", Chat: 101, Target: beforeCard.ID, Reference: reference,
			}))
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'family'='passes' AND state='pending'
 ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
			queued := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
			if kind == "newer_revision" {
				_, err = f.db.Exec(t.Context(), `UPDATE bot.pass_views SET revision=revision+1 WHERE owner='alice'`)
				require.NoError(t, err)
			}
			hit, generations := false, 0
			f.b.API.HTTP = &http.Client{
				Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
					response, requestErr := http.DefaultTransport.RoundTrip(request)
					if request.URL.Path == "/v1/me/history/generation" {
						generations++
						if kind == "target_obsolete" && generations == 2 {
							hit = true
							_, err = f.db.Exec(
								t.Context(),
								`UPDATE bot.pass_views SET message_id=message_id+10000 WHERE owner='alice'`,
							)
							require.NoError(t, err)
						}
					}
					return response, requestErr
				}),
			}
			pumpBotDeliveries(t, f.b)
			if kind == "target_obsolete" {
				require.True(t, hit)
			}
			require.Equal(
				t,
				before,
				chatMessages(t, f, 101),
				"generic stale must not edit or replace the authorized card",
			)
			var redacted bool
			var callbacks int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT COALESCE((state->>'redacted')::boolean,false)
 FROM bot.pass_views WHERE owner='alice'`).Scan(&redacted))
			require.False(t, redacted)
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice'`).
					Scan(&callbacks),
			)
			require.Positive(t, callbacks)
			cancelled, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, queued, false)
			require.NoError(t, err)
			require.Equal(t, delivery.Cancelled, cancelled.State)
			require.Equal(t, reference, cancelled.Reference)
		})
	}
}

func runPassReconstructionBoundary(t *testing.T, stage string) {
	t.Helper()
	f := passMenuFixture(t)
	var err error
	run := runPassVM(t, f, 72400, 101, "Show my pass",
		`return tools.passes.registration.show({event:"dance",view:"home"});`)
	require.Empty(t, run.Error)
	pumpBotDeliveries(t, f.b)
	before := chatMessages(t, f, 101)
	beforeCard := passMenuCard(t, f, 101)
	var originalSource string
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT (state->'source')::text FROM bot.pass_views WHERE owner='alice'`).Scan(&originalSource))
	require.NotEmpty(t, originalSource)
	if strings.HasSuffix(stage, "_grant") {
		_, err = f.db.Exec(
			t.Context(),
			`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice');
 UPDATE bot.pass_views SET state=jsonb_set(state,'{view}','"payment_queue"') WHERE owner='alice'`,
		)
		require.NoError(t, err)
	}
	revoke := func() {
		if strings.HasSuffix(stage, "_grant") {
			_, err = f.db.Exec(
				t.Context(),
				`DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='alice'`,
			)
			require.NoError(t, err)
			return
		}
		deletePassDeliveryHistory(t, f, "alice")
	}
	require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, i18n.RegistrationSaved))
	var operation, effect string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
		 WHERE owner='alice' AND reference->>'family'='passes' AND state='pending'
		 ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
	ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	admitted, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	state := installPassReconstructionTransport(t, f, stage, admitted, revoke)
	pumpBotDeliveries(t, f.b)
	require.True(t, state.hit, "revoke only after a valid reconstruction reached its domain read")
	require.Zero(t, state.sends, "retirement must never send a replacement card")
	expectedEdits := 1
	if stage == "admission_grant" {
		expectedEdits = 0 // The actual prior home payload remains authorized.
	}
	if strings.HasPrefix(stage, "receipt") {
		expectedEdits = 2 // The successful private edit, then its retirement.
	}
	require.Equal(t, expectedEdits, state.edits, "the original card must receive its retirement edit")
	require.Len(t, chatMessages(t, f, 101), len(before))
	card := passMenuCard(t, f, 101)
	require.Equal(t, beforeCard.ID, card.ID)
	if stage == "admission_grant" {
		require.Equal(t, beforeCard, card)
	} else {
		require.Empty(t, card.Markup.Rows)
		require.Contains(t, card.Text, "/passes")
	}
	var source string
	var redacted bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT (state->'source')::text,
		 COALESCE((state->>'redacted')::boolean,false) FROM bot.pass_views WHERE owner='alice'`).Scan(&source, &redacted))
	require.Equal(t, stage != "admission_grant", redacted)
	require.Equal(t, originalSource, source)
	var buttons int
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT count(*) FROM bot.pass_buttons WHERE owner='alice'`).Scan(&buttons))
	if stage == "admission_grant" {
		require.Positive(t, buttons)
	} else {
		require.Zero(t, buttons)
	}
	cancelled, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	expectedState := delivery.Cancelled
	if strings.HasPrefix(stage, "receipt") {
		expectedState = delivery.Succeeded
		require.True(t, cancelled.ContinuationDone)
		require.Equal(t, beforeCard.ID, cancelled.MessageID)
	}
	require.Equal(t, expectedState, cancelled.State)
	require.Equal(t, admitted.Reference, cancelled.Reference)
	plan, err := (interaction.Store{DB: f.db}).Load(t.Context(), "alice", 72400)
	require.NoError(t, err)
	if !strings.HasSuffix(stage, "_grant") {
		require.Equal(t, interaction.HistoryDeleted, plan.TerminalReason)
	}
}

type passReconstructionTransport struct {
	hit                        bool
	historyReads, sends, edits int
}

func installPassReconstructionTransport(
	t *testing.T,
	f *fixture,
	stage string,
	admitted botdelivery.Intent,
	revoke func(),
) *passReconstructionTransport {
	t.Helper()
	state := &passReconstructionTransport{}
	f.b.API.HTTP = &http.Client{
		Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
			response, requestErr := http.DefaultTransport.RoundTrip(request)
			if request.URL.Path == "/v1/me/history/generation" {
				state.historyReads++
			}
			boundary := stage == "domain" && request.URL.Path == "/v1/passes/events/dance/me"
			boundary = boundary ||
				(strings.HasPrefix(stage, "admission") && request.URL.Path == "/v1/me/history/generation" && state.historyReads == 2)
			if requestErr == nil && !state.hit && boundary {
				if strings.HasPrefix(stage, "admission") {
					var buttons int
					require.NoError(
						t,
						f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND revision=$1`, admitted.Reference.Revision).
							Scan(&buttons),
					)
					require.Positive(t, buttons, "revoke after menu storage and callback construction")
				}
				state.hit = true
				revoke()
			}
			return response, requestErr
		}),
	}

	f.b.TG.HTTP = &http.Client{
		Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
			if strings.HasSuffix(request.URL.Path, "/sendMessage") {
				state.sends++
			}
			if strings.HasSuffix(request.URL.Path, "/editMessageText") {
				state.edits++
			}
			response, requestErr := http.DefaultTransport.RoundTrip(request)
			if requestErr == nil && !state.hit && strings.HasPrefix(stage, "receipt") &&
				strings.HasSuffix(request.URL.Path, "/editMessageText") {
				state.hit = true
				revoke()
			}
			return response, requestErr
		}),
	}

	return state
}
