package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestConversationAuthorityReaderLocksOnlySelectedSources(t *testing.T) {
	t.Parallel()
	s, authority := conversationAuthorityFixture(t)
	require.NoError(
		t,
		s.AppendDerived(
			t.Context(),
			"alice",
			"older",
			"older private reply",
			0,
			readsource.Registration([]passbooking.ReadAuthority{authority}),
		),
	)
	require.NoError(t, s.AppendOriginal(t.Context(), "alice", "latest", "user", "recent original"))
	tx, err := s.DB.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = tx.Exec(t.Context(), `SELECT id FROM core.pass_events WHERE id='dance' FOR UPDATE`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	window, err := s.Window(ctx, "alice", 1)
	require.NoError(t, err, "the recent owner window must not lock an older unrelated source")
	require.Len(t, window.Recent, 1)
	assert.Equal(t, "recent original", window.Recent[0].Text)
	require.NoError(t, s.AppendOriginal(ctx, "alice", "new-original", "user", "new original"))
}

func TestConversationAuthorityPrivilegedGrantAndTarget(t *testing.T) {
	t.Parallel()
	for _, revoke := range []string{"grant", "target"} {
		t.Run(revoke, func(t *testing.T) {
			t.Parallel()
			s, booking := conversationAuthorityFixture(t)
			authority := passbooking.ReadAuthority{
				Kind:             passbooking.ReadPrivileged,
				Event:            "dance",
				Action:           "admin_assign",
				TargetTelegramID: 101,
				Owner:            booking.Owner,
				Version:          booking.Version,
				CreatedAt:        booking.CreatedAt,
			}
			require.NoError(
				t,
				s.AppendDerived(
					t.Context(),
					"bob",
					"privileged",
					"private privileged target",
					0,
					readsource.Registration([]passbooking.ReadAuthority{authority}),
				),
			)
			query := `DELETE FROM core.pass_booking_admins WHERE owner='bob'`
			if revoke == "target" {
				query = `UPDATE core.users SET can_book=false WHERE id='alice'`
			}
			_, err := s.DB.Exec(t.Context(), query)
			require.NoError(t, err)
			page, err := s.Read(t.Context(), "bob", conversation.Query{Limit: 1})
			require.NoError(t, err)
			require.Len(t, page.Events, 1)
			assert.True(t, page.Events[0].Omitted)
			assert.Empty(t, page.Events[0].Text)
			assert.EqualValues(t, 1, page.Generation)
		})
	}
}
