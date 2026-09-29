package conversation_test

import (
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func TestExplicitArchiveProvenance(t *testing.T) {
	t.Parallel()
	service := historyDatabase(t)
	original := strings.Repeat("Python assistant correspondence 🌍 ", 400)
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "python-assistant", "assistant", original))
	require.NoError(
		t,
		service.AppendDerived(
			t.Context(),
			"alice",
			"runtime-answer",
			"Derived answer",
			0,
			[]readsource.Authority{},
		),
	)
	require.NoError(t, service.AppendTrustedOutcome(t.Context(), "alice", "host-notice", "Booking completed"))
	for _, expected := range []struct {
		key, kind, origin string
		authority         bool
	}{
		{"python-assistant", "assistant", "original", false},
		{"runtime-answer", "assistant", "derived", true},
		{"host-notice", "system", "trusted", false},
	} {
		var kind, origin string
		var authority bool
		require.NoError(
			t,
			service.DB.QueryRow(t.Context(), `SELECT kind,origin,EXISTS(SELECT 1 FROM core.conversation_read_authorities a WHERE a.event_id=e.id) FROM core.conversation_events e WHERE owner='alice' AND source_key=$1`, expected.key).
				Scan(&kind, &origin, &authority),
		)
		require.Equal(t, expected.kind, kind)
		require.Equal(t, expected.origin, origin)
		require.Equal(t, expected.authority, authority)
	}
	var body string
	require.NoError(
		t,
		service.DB.QueryRow(t.Context(), `SELECT body FROM core.conversation_message_bodies b JOIN core.conversation_events e ON e.id=b.event_id WHERE source_key='python-assistant'`).
			Scan(&body),
	)
	require.Equal(t, original, body)
}

func TestDerivedArchiveRejectsMissingProvenanceBeforeStorage(t *testing.T) {
	t.Parallel()
	service := conversation.Service{}
	require.ErrorContains(
		t,
		service.AppendDerived(t.Context(), "alice", "missing", "model", 0, nil),
		"invalid derived conversation provenance",
	)
	require.ErrorContains(
		t,
		service.AppendDerived(t.Context(), "alice", "negative", "model", -1, []readsource.Authority{}),
		"invalid derived conversation provenance",
	)
	require.ErrorContains(
		t,
		service.AppendOriginal(t.Context(), "alice", "notice", "system", "notice"),
		"invalid original conversation kind",
	)
}
