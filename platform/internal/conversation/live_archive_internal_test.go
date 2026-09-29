package conversation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestLiveArchiveMeasuresCompleteEscapedEnvelope(t *testing.T) {
	t.Parallel()
	generation := int64(0)
	for _, build := range []func(string) any{
		func(text string) any { return OriginalArchive{SourceKey: "key<", Kind: "manual", Text: text} },
		func(text string) any { return OutcomeArchive{SourceKey: "key<", Text: text} },
		func(text string) any {
			return DerivedArchive{SourceKey: "tg-assistant-1", Text: text, ReplyToUpdateID: 1,
				ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{{Causal: &readsource.CausalSource{
					Actor: "alice", Generation: &generation, Authorities: []readsource.Authority{},
				}}}}
		},
	} {
		prefix := strings.Repeat("\"\\\n<>&\u2028🌍", 100)
		encoded, err := json.Marshal(build(prefix))
		require.NoError(t, err)
		text := prefix + strings.Repeat("x", MaxLiveArchiveBytes-len(encoded))
		require.True(t, validLiveArchive(build(text)))
		require.False(t, validLiveArchive(build(text+"x")))
	}
}

func TestLiveArchiveRejectsOversizedTextBeforeDatabase(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", MaxLiveArchiveBytes+1)
	requireHostArchiveInvalid(t, (Service{}).ArchiveOriginal(t.Context(), "alice", "key", "user", text))
	requireHostArchiveInvalid(t, (Service{}).ArchiveOutcome(t.Context(), "alice", "key", text))
	generation := int64(0)
	requireHostArchiveInvalid(t, (Service{}).ArchiveDerived(t.Context(), "alice", DerivedArchive{
		SourceKey: "tg-assistant-1", Text: text, ReplyToUpdateID: 1, Media: true,
		ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{},
	}))
}
