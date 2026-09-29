package migrate_test

import (
	"os"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventPlanRejectsConcatenatedUnknownKeys(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, record, blocker string }{
		{
			"event",
			strings.Replace(syntheticEvent, `"key":`, `"key finish_date":"unmapped","key":`, 1),
			"event_field_unmapped",
		},
		{
			"tier",
			strings.Replace(syntheticEvent, `"amount":10`, `"amount price":10,"amount":10`, 1),
			"event_tier_invalid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, path, links, plan := eventInputs(t, tc.record)
			assert.Contains(t, plan.Events[0].Blockers, tc.blocker)
			_, err := migrate.ApplyEvents(t.Context(), "not-a-dsn", stage, path, links, migrate.DefaultLimits())
			require.EqualError(t, err, "apply_record_blocked")
		})
	}
}

func TestEventApplyRejectsConcatenatedResolutionKeys(t *testing.T) {
	t.Parallel()
	for _, field := range []string{`"version plan_sha256":1,`, `"legacy_key display_order":0,`} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			stage, path, links, _ := eventInputs(t, syntheticEvent)
			raw, err := os.ReadFile(links)
			require.NoError(t, err)
			before := `"version":`
			if strings.Contains(field, "legacy_key") {
				before = `"legacy_key":`
			}
			raw = []byte(strings.Replace(string(raw), before, field+before, 1))
			require.NoError(t, os.WriteFile(links, raw, 0o600))
			_, err = migrate.ApplyEvents(t.Context(), "not-a-dsn", stage, path, links, migrate.DefaultLimits())
			require.EqualError(t, err, "resolution_invalid")
		})
	}
}

func TestEventPlanRequiresLosslessStrictInstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value    string
		accepted bool
	}{
		{"2026-12-01T00:00:00+00:60", false},
		{"2026-12-01T00:00:00-00:60", false},
		{"2026-12-01T00:00:00+24:00", false},
		{"2026-12-01T00:00:00.1234560001Z", false},
		{"2026-12-01T00:00:00.123456001Z", false},
		{"2026-12-01T00:00:00.1234560Z", false},
		{"2026-12-01T0:00:00Z", false},
		{"2026-12-01T00:00:00,123456Z", false},
		{"2026-12-01T00:00:00.Z", false},
		{"Z", false},
		{"", false},
		{"2026-12-01T00:00:00.123456Z", true},
		{"2026-12-01T00:00:00.000000Z", true},
		{"2026-12-01T00:00:00.1+00:00", true},
		{"2026-12-01T00:00:00+23:59", true},
		{"2026-12-01T00:00:00-03:30", true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			record := strings.Replace(syntheticEvent, "2026-12-01T03:00:00+03:00", tc.value, 1)
			_, _, _, plan := eventInputs(t, record)
			if tc.accepted {
				require.NotNil(t, plan.Events[0].Candidate)
				return
			}
			assert.Contains(t, plan.Events[0].Blockers, "event_datetime_unresolved")
		})
	}
}

func TestUserApplyRejectsConcatenatedResolutionKeys(t *testing.T) {
	t.Parallel()
	stage, path, links := applyInputs(t, `{"_id":"one","bot_id":77,"user_id":101,"print_name":"One"}`)
	raw, err := os.ReadFile(links)
	require.NoError(t, err)
	raw = []byte(strings.Replace(string(raw), `"legacy_key":`, `"legacy_key owner":"unmapped","legacy_key":`, 1))
	require.NoError(t, os.WriteFile(links, raw, 0o600))
	_, err = migrate.ApplyUsers(t.Context(), "not-a-dsn", stage, path, links, migrate.DefaultLimits())
	require.EqualError(t, err, "resolution_invalid")
}

func TestEventPlanRejectsInvalidInstantsInEveryDateField(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"2026-12-01T00:00:00+00:60", "2026-12-01T00:00:00.1234560001Z"} {
		for _, source := range []string{`{"$date":"2026-12-01T03:00:00+03:00"}`, `"2026-09-01T00:00:00Z"`, `"2026-10-01T00:00:00Z"`} {
			t.Run(invalid+source, func(t *testing.T) {
				t.Parallel()
				record := strings.Replace(syntheticEvent, source, `"`+invalid+`"`, 1)
				_, _, _, plan := eventInputs(t, record)
				assert.Contains(t, plan.Events[0].Blockers, "event_datetime_unresolved")
			})
		}
	}
}
