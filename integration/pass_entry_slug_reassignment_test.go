package integration_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPassEntryAcceptedSlugStaysWithOriginalEvent(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	user := f.user(t, 29104)
	original := f.event
	otherDoc := original.Document
	otherDoc.Name, otherDoc.Slug = "Other Pass Sale", "other-sale"
	other, err := f.h.store.ChangeEvent(t.Context(), f.scope, "sale-qa", uuid.NewString(), db.EventChange{
		ID: uuid.New(), Document: otherDoc,
	})
	require.NoError(t, err)

	require.NoError(t, f.h.service.Stop(t.Context()))
	accepted, _ := f.accept(t, user,
		`/passes synthetic --role leader --legal-name "Synthetic Full Name" --passport-number SYNTHETIC-42`, 0, "")
	waiting, err := f.h.store.GetTask(t.Context(), accepted)
	require.NoError(t, err)
	require.Equal(t, "accepted", waiting.State)
	selected, expires, err := f.priority(t, user)
	require.NoError(t, err, "intake holds promo for the event addressed at acceptance")
	require.Equal(t, time.Hour, expires.Sub(selected))

	renamed := original.Document
	renamed.Slug = "renamed-sale"
	f.saveEvent(t, renamed)
	otherDoc.Slug = original.Document.Slug
	_, err = f.h.store.ChangeEvent(t.Context(), f.scope, "sale-qa", uuid.NewString(), db.EventChange{
		ID: other.ID, ExpectedVersion: other.Version, Document: otherDoc,
	})
	require.NoError(t, err)
	f.h.server.Close()
	f.h.start(t)
	f.h.await(t, accepted, "completed")

	entry, originalErr := f.h.store.Registration(t.Context(), f.scope, original.ID, user.app.UserID)
	_, otherErr := f.h.store.Registration(t.Context(), f.scope, other.ID, user.app.UserID)
	require.ErrorIs(t, otherErr, pgx.ErrNoRows, "the reassigned slug does not authorize registration for the other event")
	require.NoError(t, originalErr, "the accepted request still registers for the original event")
	require.Equal(t, registrations.Leader, entry.Role)
	f.pass(t, user, "promo", 7000)
	selectedAfter, expiresAfter, err := f.priority(t, user)
	require.NoError(t, err)
	require.Equal(t, selected, selectedAfter)
	require.Equal(t, expires, expiresAfter)
	var otherHolds int
	err = f.h.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2",
		other.ID, user.app.UserID).Scan(&otherHolds)
	require.NoError(t, err)
	require.Zero(t, otherHolds)
	f.noLoom(t)
}
