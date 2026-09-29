package migrate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassConversionPreservesStaleReceiptAndActorAbsence(t *testing.T) {
	t.Parallel()
	raw := []byte(
		`{"user_id":101,"pass_key":"event","state":"assigned","role":"leader","type":"guest","date_created":"2026-09-01T00:00:00Z","date_assignment":"2026-09-05T00:00:00Z","price":100,"proof_admin":202,"proof_file":"previous.jpg","proof_received":"2026-09-02T00:00:00Z","proof_rejected":"2026-09-03T00:00:00Z"}`,
	)
	candidate, err := convertPass(raw)
	require.NoError(t, err)
	assert.False(t, candidate.ActiveReceipt)
	assert.Equal(t, "previous.jpg", candidate.ProofFile)
	require.NotNil(t, candidate.Rejected)
	assert.Zero(t, candidate.ReviewingAdmin)
}
func TestPassRuntimeProjectionDoesNotDuplicatePrivateUserFields(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(
		`{"_id":"user","event":{"state":"paid","comment":"pass comment"},"memo":"private unrelated memo","passport_number":"private passport","notified_passport_data_required":null,"proof_admins":{"event":202}}`,
	)
	row := PassPlanRecord{
		Source: usersSource,
		Record: raw,
		Field:  "event",
		Legacy: UserLegacyReference{RecordSHA256: "parent-digest", File: "users.jsonl", Line: 1},
	}
	projected := string(passRuntimeRecord(row))
	assert.Contains(t, projected, "pass comment")
	assert.Contains(t, projected, "parent-digest")
	assert.NotContains(t, projected, "private unrelated memo")
	assert.NotContains(t, projected, "private passport")
	row.Field = ""
	projected = string(passRuntimeRecord(row))
	assert.Contains(t, projected, `"notified_passport_data_required":null`)
	assert.Contains(t, projected, `"passport_field_present":true`)
	assert.NotContains(t, projected, "private passport")
	assert.NotContains(t, projected, "pass comment")
}
func TestPassPairValidationAcceptsPreservableMixedStates(t *testing.T) {
	t.Parallel()
	now := time.Now()
	price := int64(100)
	a := &PassCandidate{
		Event:      "e",
		TelegramID: 101,
		Partner:    202,
		Admin:      303,
		State:      "assigned",
		Assigned:   &now,
		Price:      &price,
		Role:       "leader",
	}
	b := &PassCandidate{
		Event:      "e",
		TelegramID: 202,
		Partner:    101,
		Admin:      303,
		State:      "paid",
		Assigned:   &now,
		Price:      &price,
		Role:       "leader",
	}
	p := preparedPasses{
		Users:    map[int64]OrderDependency{101: {}, 202: {}, 303: {}},
		Bookings: map[string][]PassPlanRecord{"e": {{Candidate: a}, {Candidate: b}}},
	}
	require.NoError(t, p.validatePassPairs())
	b.Partner = 404
	require.EqualError(t, p.validatePassPairs(), "pass_pair_conflict_correct_source_before_import")
}

func TestPassEmbeddedPrecedenceAndDeferredPrivacy(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(
		`{"_id":"user","bot_id":77,"user_id":101,"print_name":"Synthetic","event":{"state":"paid","obsolete":true},"notified_passport_data_required":false}`,
	)
	p := PassPlan{BotID: 77}
	row := OrderPlanRecord{
		Source: usersSource,
		Record: raw,
		Legacy: UserLegacyReference{
			Key:          "original-key",
			RecordID:     json.RawMessage(`"user"`),
			RecordSHA256: hashBytes(raw),
		},
	}
	require.NoError(t, p.addPassUser(row, map[string]bool{"event": true}, map[string]bool{"event:101": true}))
	require.Len(t, p.Records, 2)
	assert.True(t, p.Records[0].Shadowed)
	assert.Empty(t, p.Records[0].Blockers)
	assert.JSONEq(t, string(raw), string(p.Records[0].Record))
	var fields map[string]json.RawMessage
	clean := []byte(
		`{"_id":"user","bot_id":77,"user_id":101,"print_name":"Synthetic","event":{"state":"waitlist","role":"leader"},"notified_passport_data_required":null}`,
	)
	require.NoError(t, json.Unmarshal(clean, &fields))
	planned := convertUser(fields, 77)
	deferUserPassFields(fields, &planned, map[string]bool{"event": true})
	assert.NotContains(t, planned.Blockers, "unmapped_fields")
	assert.NotEmpty(t, planned.DeferredPasses)
	assert.NotContains(t, string(planned.DeferredPasses), "print_name")
	planned = convertUser(fields, 77)
	deferUserPassFields(fields, &planned, map[string]bool{})
	assert.Contains(t, planned.Blockers, "unmapped_fields", "unknown event-key objects must not bypass user mapping")
}

func TestPassReviewFactsAndNullableBalance(t *testing.T) {
	t.Parallel()
	base := `{"user_id":101,"pass_key":"event","state":"paid","role":"leader","date_created":"2026-09-01T00:00:00Z","date_assignment":"2026-09-02T00:00:00Z","price":100,"proof_admin":202,"proof_file":"file.jpg","proof_received":"2026-09-03T00:00:00Z","skip_in_balance_count":null`
	for _, test := range []struct {
		name, suffix, decision string
		known                  bool
	}{
		{"pending", `}`, "pending", false},
		{"accepted unknown", `,"proof_accepted":"2026-09-04T00:00:00Z"}`, "accepted", false},
		{"accepted known", `,"proof_accepted":"2026-09-04T00:00:00Z","proof_admin_accepted":202}`, "accepted", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate, err := convertPass([]byte(base + test.suffix))
			require.NoError(t, err)
			assert.Nil(t, candidate.SkipBalance)
			assert.Zero(t, candidate.ReceivingAdmin, "current contact is not evidence of receiving actor")
			decision, at, actor := passReview(candidate, map[string]string{"202": "real-admin"})
			assert.Equal(t, test.decision, decision)
			if test.decision == "pending" {
				assert.Nil(t, at)
			} else {
				require.NotNil(t, at)
			}
			if test.known {
				require.NotNil(t, actor)
				assert.Equal(t, "real-admin", *actor)
			} else {
				assert.Nil(t, actor)
			}
		})
	}
	rejected := strings.ReplaceAll(
		base,
		`"state":"paid"`,
		`"state":"assigned"`,
	) + `,"proof_rejected":"2026-09-04T00:00:00Z"}`
	candidate, err := convertPass([]byte(rejected))
	require.NoError(t, err)
	decision, at, actor := passReview(candidate, nil)
	assert.Equal(t, "rejected", decision)
	assert.NotNil(t, at)
	assert.Nil(t, actor)
}

func TestPassFoodMarkersRetainStrictDeferredEvidence(t *testing.T) {
	t.Parallel()
	raw := `{"user_id":101,"pass_key":"event","state":"waitlist","role":"leader","proof_admin":202,"date_created":"2026-09-01T00:00:00Z","notified_food_first":false,"notified_food_last":true}`
	_, err := convertPass([]byte(raw))
	require.NoError(t, err)
	_, err = convertPass([]byte(strings.ReplaceAll(raw, `"notified_food_first":false`, `"notified_food_first":null`)))
	require.EqualError(t, err, "pass_food_marker_invalid")
}
