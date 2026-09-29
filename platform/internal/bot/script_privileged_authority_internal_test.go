package bot

import (
	"encoding/json"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestPrivilegedReadEvidenceSurvivesEmptyResultAndRestart(t *testing.T) {
	t.Parallel()
	for _, name := range []string{scriptPaymentHistory, scriptPaymentQueue, scriptPractitionerSchedule, scriptPractitionerPreferences, scriptPractitionerBookings} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			record, err := prepareScriptPrivilegedRead(
				t.Context(),
				"actor",
				1,
				scriptclient.ToolCall{Name: name, Arguments: json.RawMessage(`{"event":"event-a"}`)},
				agent.Input{},
			)
			require.NoError(t, err)
			_, err = (&Bot{}).scriptHost().EncodeResult(
				&record,
				map[string]any{"items": []any{}, "more": false},
				"",
				maxScriptReadBytes,
			)
			require.NoError(t, err)
			raw, err := json.Marshal(record)
			require.NoError(t, err)
			var resumed agenthost.ScriptToolRecord
			require.NoError(t, json.Unmarshal(raw, &resumed))
			refs, err := agenthost.ScriptCallReadAuthorities("actor", resumed)
			require.NoError(t, err)
			require.Len(t, refs, 1)
			if name == scriptPaymentHistory || name == scriptPaymentQueue {
				require.Equal(
					t,
					passbooking.ReadAuthority{Kind: passbooking.ReadPaymentRole, Event: "event-a"},
					refs[0].Registration,
				)
			} else {
				require.Equal(t, massage.ReadAuthority{Event: "event-a", Owner: "actor"}, refs[0].Practitioner)
			}
			resumed.PrivilegedRead = nil
			_, err = agenthost.ScriptCallReadAuthorities("actor", resumed)
			require.Error(t, err, "cached evidence cannot conceal a missing required carrier")
			require.True(t, agenthost.ScriptCallHasAuthorities(resumed))
		})
	}
}

func TestPrivilegedEventPageRetainsEveryScopeAndEmptyAdmission(t *testing.T) {
	t.Parallel()
	paymentRole := core.PrivilegedReadCapabilities{PaymentReads: true}
	bothRoles := core.PrivilegedReadCapabilities{PaymentReads: true, PractitionerReads: true}
	admission := agenthost.PrivilegedEventAuthorities(
		"actor",
		[]core.PrivilegedReadEvent{{Event: "a", PrivilegedReadCapabilities: paymentRole}},
	)
	for _, items := range [][]core.PrivilegedReadEvent{nil, {{Event: "b", PrivilegedReadCapabilities: bothRoles}}} {
		record := agenthost.ScriptToolRecord{
			PrivilegedRead: &agenthost.ScriptPrivilegedRead{Owner: "actor", Admission: admission},
			Outcome:        agent.ScriptToolResult{Name: scriptPrivilegeEvents},
		}
		_, err := (&Bot{}).scriptHost().EncodeResult(
			&record,
			core.ReadPage[core.PrivilegedReadEvent]{Items: items},
			"",
			maxScriptReadBytes,
		)
		require.NoError(t, err)
		refs, err := agenthost.ScriptCallReadAuthorities("actor", record)
		require.NoError(t, err)
		require.Len(t, refs, 1+2*len(items))
		require.Contains(t, refs, admission[0])
		record.PrivilegedRead.Admission = nil
		_, err = agenthost.ScriptCallReadAuthorities("actor", record)
		require.Error(t, err)
	}
}

func TestFoodReviewEvidenceRetainsHostEventOnEmptyPage(t *testing.T) {
	t.Parallel()
	for _, name := range []string{scriptFoodReviewQueue, scriptFoodReviewRead} {
		record := agenthost.ScriptToolRecord{
			Food:    &legacyfood.Command{EventID: "food-a"},
			Outcome: agent.ScriptToolResult{Name: name},
		}
		_, err := (&Bot{}).scriptHost().EncodeResult(&record, map[string]any{"items": []any{}}, "", maxScriptReadBytes)
		require.NoError(t, err)
		refs, err := agenthost.ScriptCallReadAuthorities("actor", record)
		require.NoError(t, err)
		require.Equal(
			t,
			[]readsource.Authority{{Food: legacyfood.ReadAuthority{Event: "food-a", Scope: "review"}}},
			refs,
		)
		record.Food = nil
		_, err = agenthost.ScriptCallReadAuthorities("actor", record)
		require.Error(t, err)
	}
}
