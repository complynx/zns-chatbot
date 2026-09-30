package migrate_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func TestMessagesFullHistoryPolicy(t *testing.T) {
	t.Parallel()
	for _, instant := range []string{"1970-01-01T00:00:00Z", "2099-12-31T23:59:59Z"} {
		t.Run(instant, func(t *testing.T) {
			t.Parallel()
			stage, plan, path, _, r := messageInputs(t, syntheticMessage)
			r.Retention, r.RetainFrom, r.RetainUntil = "all", "", ""
			r.Messages[0].ResolvedAt = instant
			writeMessageResolutions(t, path, r)
			result, err := migrate.ValidateMessages(stage, plan, path, migrate.DefaultLimits())
			require.NoError(t, err)
			require.Equal(t, 1, result.Retained)
			for _, reason := range []string{"private", "sensitive", "expired", "unresolved_omitted"} {
				r.Messages[0].Disposition, r.Messages[0].Provenance = "omitted", reason
				writeMessageResolutions(t, path, r)
				result, err = migrate.ValidateMessages(stage, plan, path, migrate.DefaultLimits())
				require.NoError(t, err)
				require.Equal(t, 1, result.Omitted)
			}
		})
	}
}

func TestMessagesFullHistoryRejectsContradictions(t *testing.T) {
	t.Parallel()
	cases := map[string]func(map[string]any){
		"from":                  func(r map[string]any) { r["retain_from"] = "1970-01-01T00:00:00Z" },
		"until":                 func(r map[string]any) { r["retain_until"] = "2099-01-01T00:00:00Z" },
		"empty_bound":           func(r map[string]any) { r["retain_from"] = "" },
		"null_bound":            func(r map[string]any) { r["retain_until"] = nil },
		"unknown":               func(r map[string]any) { r["retention"] = "everything" },
		"empty_policy":          func(r map[string]any) { r["retention"] = "" },
		"null_policy":           func(r map[string]any) { r["retention"] = nil },
		"window_without_bounds": func(r map[string]any) { r["retention"] = "window" },
		"outside_window": func(r map[string]any) {
			row := r["messages"].([]any)[0].(map[string]any)
			row["disposition"], row["provenance"] = "excluded", "outside_window"
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stage, plan, path, _, r := messageInputs(t, syntheticMessage)
			r.Retention, r.RetainFrom, r.RetainUntil = "all", "", ""
			raw, err := json.Marshal(r)
			require.NoError(t, err)
			var input map[string]any
			require.NoError(t, json.Unmarshal(raw, &input))
			change(input)
			raw, err = json.Marshal(input)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, raw, 0600))
			_, err = migrate.ValidateMessages(stage, plan, path, migrate.DefaultLimits())
			require.Error(t, err)
		})
	}
}

func TestMessagesRejectAbsentAndWindowRetention(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", "window"} {
		t.Run("mode_"+mode, func(t *testing.T) {
			t.Parallel()
			stage, plan, path, _, r := messageInputs(t, syntheticMessage)
			r.Retention = mode
			writeMessageResolutions(t, path, r)
			_, err := migrate.ValidateMessages(stage, plan, path, migrate.DefaultLimits())
			require.EqualError(t, err, "message_retention_invalid")
			r.RetainFrom, r.RetainUntil = "2035-01-01T00:00:00Z", "2036-01-01T00:00:00Z"
			r.Messages[0].Disposition, r.Messages[0].Provenance = "excluded", "outside_window"
			r.Messages[0].ResolvedAt = "1970-01-01T00:00:00Z"
			writeMessageResolutions(t, path, r)
			_, err = migrate.ValidateMessages(stage, plan, path, migrate.DefaultLimits())
			require.EqualError(t, err, "message_retention_invalid")
		})
	}
}

func TestMessagesFullHistoryOwnerExclusion(t *testing.T) {
	t.Parallel()
	stage, plan, path, _, r := messageInputs(t, strings.Replace(syntheticMessage, "2035-06-01", "1970-01-01", 1))
	r.Retention, r.RetainFrom, r.RetainUntil = "all", "", ""
	r.Messages[0].Owner = ""
	r.Messages[0].Disposition, r.Messages[0].Provenance = "excluded", "owner_excluded"
	writeMessageResolutions(t, path, r)
	result, err := migrate.ValidateMessages(stage, plan, path, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, 1, result.Excluded)
}
