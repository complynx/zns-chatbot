package integration_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/passcopy"
)

func TestPassSalePairChoiceReleasesSoloPriority(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	first, second := f.user(t, 26601), f.user(t, 26602)

	menu := f.menu(t, first)
	after := f.click(t, first, menu)
	after = f.click(t, first, f.button(t, first, after, passcopy.Text("en", "entry.leader")))
	choice := f.button(t, first, after, passcopy.Text("en", "entry.create_pair"))
	f.click(t, first, f.button(t, first, after, passcopy.Text("en", "entry.join_pair")))
	require.True(t, passSaleHoldsPromo(t, f, first), "reading pair code instructions preserves solo priority")

	forged, _ := f.accept(t, first, "", choice.messageID+1, choice.button.CallbackData)
	f.h.await(t, forged, "completed")
	require.True(t, passSaleHoldsPromo(t, f, first), "wrong receipt cannot release priority")

	secondForm := f.solo(t, second, f.click(t, second, f.menu(t, second)))
	f.completeDetails(t, second, secondForm)
	require.Nil(t, f.registration(t, second).Pass, "the earlier incomplete solo applicant still holds the slot")

	pairTask, pairAfter := f.accept(t, first, "", choice.messageID, choice.button.CallbackData)
	require.False(t, passSaleHoldsPromo(t, f, first), "an accepted pair choice releases the solo hold at intake")
	f.h.await(t, pairTask, "completed")
	f.button(t, first, pairAfter, passcopy.Text("en", "entry.details_form"))
	_, err := f.h.store.Registration(t.Context(), f.scope, f.event.ID, first.app.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the pair intake is still incomplete")
	f.pass(t, second, "promo", 7000)

	f.click(t, first, menu)
	f.click(t, first, choice)
	require.False(t, passSaleHoldsPromo(t, f, first), "old event and pair controls cannot restore the same hold")
	f.noLoom(t)
}

func TestPassSaleExplicitPairReleasesPriorSoloPriority(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	first := f.user(t, 26603)
	after := f.click(t, first, f.menu(t, first))
	after = f.click(t, first, f.button(t, first, after, passcopy.Text("en", "entry.leader")))
	stale := f.button(t, first, after, passcopy.Text("en", "entry.create_pair"))
	doc := f.event.Document
	doc.Name += " Updated"
	f.saveEvent(t, doc)
	f.click(t, first, stale)
	require.True(t, passSaleHoldsPromo(t, f, first), "an event version mismatch cannot release priority")

	pairTask, pairAfter := f.accept(t, first, "/passes synthetic --role leader --pair", 0, "")
	require.False(t, passSaleHoldsPromo(t, f, first), "the explicit pair command releases the prior event hold")
	f.h.await(t, pairTask, "completed")
	f.button(t, first, pairAfter, passcopy.Text("en", "entry.details_form"))
	_, err := f.h.store.Registration(t.Context(), f.scope, f.event.ID, first.app.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "pair details are still incomplete")
	f.noLoom(t)
}

func TestPassSalePromoDeadlineFollowsSelectedRole(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	f := newPassQAFixture(t, passQAPromoTiers(start), true)
	user := f.user(t, 26604)
	task, after := f.accept(t, user, "/passes synthetic --role leader", 0, "")
	f.h.await(t, task, "completed")
	f.button(t, user, after, passcopy.Text("en", "entry.details_form"))
	selected, expires, err := f.priority(t, user)
	require.NoError(t, err)
	zone, err := time.LoadLocation(f.event.Document.Timezone)
	require.NoError(t, err)
	deadline := passcopy.Text("en", "queue.promoDeadline",
		expires.In(zone).Format("2006-01-02 15:04")+" ("+zone.String()+")")
	f.text(t, user, after, deadline)

	after = f.click(t, user, f.menu(t, user))
	f.text(t, user, after, deadline)
	follower := f.button(t, user, after, passcopy.Text("en", "entry.follower"))
	followerTask, _ := f.accept(t, user, "", follower.messageID, follower.button.CallbackData)
	replies := passQAReplies(t, f.h, followerTask, user.telegramID)
	require.Contains(t, replies[0].Text, passcopy.Text("en", "entry.pick_mode", f.event.Document.Name))
	require.NotContains(t, replies[0].Text, deadline)
	selectedAgain, expiresAgain, err := f.priority(t, user)
	require.NoError(t, err)
	require.Equal(t, selected, selectedAgain)
	require.Equal(t, expires, expiresAgain)
	f.noLoom(t)
}

func passSaleHoldsPromo(t *testing.T, f *passQAFixture, user passQAUser) bool {
	t.Helper()
	var held bool
	err := f.h.store.Pool().QueryRow(t.Context(), `
SELECT EXISTS (
	SELECT 1 FROM shuttle_pass_promo_choices WHERE event_id=$1 AND user_id=$2 AND NOT forfeited
)`, f.event.ID, user.app.UserID).Scan(&held)
	require.NoError(t, err)
	return held
}
