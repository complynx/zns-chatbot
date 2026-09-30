package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func domainScriptMarker(t *testing.T, f *fixture, tool, args, revocation string) (*knowledgeModel, string) {
	t.Helper()
	var marker string
	workerRuns := 0
	model := runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				workerRuns++
				data := scriptCall(ctx, t, callback, tool, args)
				marker = fmt.Sprintf("domain-source-%x", sha256.Sum256(data))
				if revocation != "" {
					_, err := f.db.Exec(ctx, revocation)
					require.NoError(t, err)
				}
				return json.Marshal(map[string]string{"derived": marker})
			},
		),
	)
	if revocation != "" {
		assertPrivilegedFreshInput(t, f, model, marker)
	}
	require.Equal(t, 1, workerRuns)
	require.Len(t, model.inputs, 2)
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	if revocation == "" {
		require.Contains(t, string(encoded), marker)
	} else {
		require.NotContains(t, string(encoded), marker)
	}
	return model, marker
}

func persistDomainSource(t *testing.T, f *fixture, model *knowledgeModel, marker, revocation string) {
	t.Helper()
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: model.inputs[1].Script.ReadAuthorities}
	service := knowledge.Service{DB: f.db}
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "domain-copy",
		Topic:   "notes",
		FactKey: "domain-copy",
		Text:    marker,
	}
	_, err := service.ExecuteDerived(t.Context(), "alice", command, source)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), revocation)
	require.NoError(t, err)
	page, err := (knowledge.Service{DB: f.db}).SearchMemory(
		t.Context(),
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "notes"},
	)
	require.NoError(t, err)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), marker)
	command.Key, command.FactKey = "late-copy", "late-copy"
	_, err = service.ExecuteDerived(t.Context(), "alice", command, source)
	require.Error(t, err)
}

func TestPrivilegedPractitionerSourceScopesAndOwner(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"massage.practitioner.schedule", "massage.practitioner.preferences", "massage.practitioner.bookings"} {
		for _, revoke := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/revoke=%t", tool, revoke), func(t *testing.T) {
				t.Parallel()
				f := setup(t)
				seedPrivilegedReads(t, f)
				_, err := f.db.Exec(
					t.Context(),
					`INSERT INTO core.massage_events(id) VALUES('other-practitioner-event');
 INSERT INTO core.massage_specialists(event_id,owner,name) VALUES('other-practitioner-event','alice','Other scope'),('sandbox-festival','visitor','Other owner')`,
				)
				require.NoError(t, err)
				const revokeRole = `DELETE FROM core.massage_work WHERE event_id='sandbox-festival' AND specialist='alice'; DELETE FROM core.massage_bookings WHERE event_id='sandbox-festival' AND specialist='alice'; DELETE FROM core.massage_specialists WHERE event_id='sandbox-festival' AND owner='alice'`
				query := ""
				if revoke {
					query = revokeRole
				}
				model, marker := domainScriptMarker(t, f, tool, `{"event":"sandbox-festival"}`, query)
				if !revoke {
					tx, beginErr := f.db.Begin(t.Context())
					require.NoError(t, beginErr)
					valid, checkErr := readsource.Lock(
						t.Context(),
						tx,
						"visitor",
						[]readsource.Authority{
							{Practitioner: massage.ReadAuthority{Event: "sandbox-festival", Owner: "alice"}},
						},
					)
					require.NoError(t, checkErr)
					require.Equal(
						t,
						[]bool{false},
						valid,
						"another practitioner cannot read the original owner's schedule",
					)
					require.NoError(t, tx.Rollback(t.Context()))
					persistDomainSource(t, f, model, marker, revokeRole)
				}
				_, err = (massage.Service{DB: f.db}).PractitionerSchedule(
					t.Context(),
					"alice",
					"other-practitioner-event",
					"",
				)
				require.NoError(t, err)
			})
		}
	}
}

func TestPrivilegedEventsScopesAndEmptyContinuation(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"payment", "practitioner", "empty"} {
		t.Run(variant, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			seedPrivilegedReads(t, f)
			args := `{}`
			revocation := `DELETE FROM core.pass_payment_admins WHERE event_id='script-dance' AND owner='alice'`
			if variant != "payment" {
				revocation = `DELETE FROM core.massage_work WHERE event_id='sandbox-festival' AND specialist='alice'; DELETE FROM core.massage_bookings WHERE event_id='sandbox-festival' AND specialist='alice'; DELETE FROM core.massage_specialists WHERE event_id='sandbox-festival' AND owner='alice'`
			}
			if variant == "empty" {
				cursor := core.EncodeReadCursor(
					core.ReadCursor{Actor: "alice", Scope: "privileges.events", Position: "zzzz"},
				)
				encoded, err := json.Marshal(map[string]string{"cursor": cursor})
				require.NoError(t, err)
				args = string(encoded)
			}
			domainScriptMarker(t, f, "privileges.events", args, revocation)
			caps, err := (core.Service{DB: f.db}).PrivilegedReads(t.Context(), "alice")
			require.NoError(t, err)
			require.True(t, caps.PaymentReads || caps.PractitionerReads)
		})
	}
}

func TestPrivilegedFoodSourceSurvivesCurrentEventSwitch(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"food.review.queue", "food.review.read"} {
		for _, revoke := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/revoke=%t", tool, revoke), func(t *testing.T) {
				t.Parallel()
				f, _, order := foodSubmittedFixture(t)
				_, err := f.db.Exec(
					t.Context(),
					`INSERT INTO core.pass_events(id,finishes_at) VALUES('food-next','2036-01-01Z');
 INSERT INTO core.food_events(event_id,bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after)
 SELECT 'food-next',bot_id,menu,menu_sha256,meal_prices,activity_prices,'2036-01-01Z',cacao_capacity,first_before,last_before,notify_after FROM core.food_events WHERE event_id='food-bot';
 INSERT INTO core.food_admins(event_id,owner,can_review) VALUES('food-bot','alice',true),('food-next','alice',true)`,
				)
				require.NoError(t, err)
				args := `{}`
				if tool == "food.review.read" {
					encoded, encodeErr := json.Marshal(map[string]string{"order_id": order.ID})
					require.NoError(t, encodeErr)
					args = string(encoded)
				}
				const revokeRole = `DELETE FROM core.food_admins WHERE event_id='food-bot' AND owner='alice'; UPDATE core.pass_events SET finishes_at=now()-interval '1 day' WHERE id='food-bot'`
				query := ""
				if revoke {
					query = revokeRole
				}
				model, marker := domainScriptMarker(t, f, tool, args, query)
				if !revoke {
					persistDomainSource(t, f, model, marker, revokeRole)
				}
				var retained bool
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT can_review FROM core.food_admins WHERE owner='alice' AND event_id='food-next'`).
						Scan(&retained),
				)
				require.True(t, retained)
			})
		}
	}
}
