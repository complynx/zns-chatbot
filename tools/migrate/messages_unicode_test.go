package migrate_test

import (
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func TestMessagesStageRejectsMalformedUnicode(t *testing.T) {
	t.Parallel()
	for name, record := range map[string]string{
		"high":          strings.Replace(syntheticMessage, "ordinary synthetic text", `\ud800`, 1),
		"low":           strings.Replace(syntheticMessage, "ordinary synthetic text", `\udfff`, 1),
		"embedded":      strings.Replace(syntheticMessage, "ordinary synthetic text", `prefix\ud800suffix`, 1),
		"unknown field": strings.TrimSuffix(syntheticMessage, "}") + `,"opaque":"\ud800"}`,
		"key":           strings.TrimSuffix(syntheticMessage, "}") + `,"\ud800":"opaque"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory, manifest := snapshot(t)
			addFile(t, directory, &manifest, "messages.jsonl", "messages", "records", []byte(record+"\n"), 1)
			writeManifest(t, directory, manifest)
			_, _, err := migrate.Stage(directory, filepath.Join(t.TempDir(), "stage"), migrate.DefaultLimits())
			require.Error(t, err)
		})
	}
}
func TestMessagesPreserveReplacementAndPairedUnicode(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{`�`: "�", `\ufffd`: "�", `\uD83D\uDE00`: "😀", `\\ud800`: `\ud800`, `\ud800\udc00`: "𐀀"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			record := strings.Replace(syntheticMessage, "ordinary synthetic text", raw, 1)
			stage, path, resolutions, plan, _ := messageInputs(t, record)
			require.Equal(t, want, plan.Messages[0].Candidate.Content)
			_, err := migrate.ValidateMessages(stage, path, resolutions, migrate.DefaultLimits())
			require.NoError(t, err)
		})
	}
}
