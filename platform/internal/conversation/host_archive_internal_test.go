package conversation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestHostArchiveRejectsInvalidOriginalAndOutcomeBeforeDatabase(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"", strings.Repeat("x", 201), "nul\x00key", string([]byte{0xff})} {
		err := (Service{}).ArchiveOriginal(t.Context(), "alice", key, "user", "text")
		requireHostArchiveInvalid(t, err)
		requireHostArchiveInvalid(t, (Service{}).ArchiveOutcome(t.Context(), "alice", key, "text"))
	}
	requireHostArchiveInvalid(t, (Service{}).ArchiveOriginal(t.Context(), "alice", "key", "assistant", "model"))
	requireHostArchiveInvalid(t, (Service{}).ArchiveOriginal(t.Context(), "alice", "key", "user", string([]byte{0xff})))
	requireHostArchiveInvalid(
		t,
		(Service{}).ArchiveOutcome(t.Context(), "alice", "key", strings.Repeat("x", MaxBodyBytes+1)),
	)
}

func TestHostDerivedArchiveRejectsMissingProvenanceBeforeDatabase(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*DerivedArchive){
		func(v *DerivedArchive) { v.ExpectedGeneration = nil },
		func(v *DerivedArchive) { *v.ExpectedGeneration = -1 },
		func(v *DerivedArchive) { v.ReadAuthorities = nil },
		func(v *DerivedArchive) { v.ReadAuthorities = []readsource.Authority{{}} },
		func(v *DerivedArchive) { v.ReplyToUpdateID = 0 },
		func(v *DerivedArchive) { v.SourceKey = "tg-assistant-2" },
	} {
		generation := int64(0)
		input := DerivedArchive{SourceKey: "tg-assistant-1", Text: "reply", ReplyToUpdateID: 1,
			ExpectedGeneration: &generation, ReadAuthorities: []readsource.Authority{}}
		change(&input)
		requireHostArchiveInvalid(t, (Service{}).ArchiveDerived(t.Context(), "alice", input))
	}
}

func requireHostArchiveInvalid(t *testing.T, err error) {
	t.Helper()
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "invalid_json", problem.Code)
}
