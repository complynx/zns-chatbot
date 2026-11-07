package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestFailedDialogueWarnsBeforeRetryAndPreservesCompletedBooking(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.question = true
			f, owner, _, change := massageBookingFixtureWithPeers(t, 2, p)
			profile, err := f.H.store.GetProfile(t.Context(), owner)
			require.NoError(t, err)
			changeProfile(t, f.H, owner, db.ProfileMutation{
				OperationID: uuid.New(), Kind: "set_language",
				ExpectedVersion: profile.Version, BotLanguage: language,
			})
			f.H.start(t)
			taskID := acceptMassageContextText(t, f.H, 97500, "Continue my massage booking")
			f.H.await(t, taskID, "awaiting_user")
			f.H.awaitReplies(t, 1)

			operation := uuid.New()
			args := massageBookingArgs(f, change, 0)
			prepared, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, operation, args)
			require.NoError(t, err)
			booking, err := f.H.store.BookOwnMassage(
				t.Context(),
				owner,
				operation,
				prepared.ID,
				args,
				db.MassageSourceAgent,
			)
			require.NoError(t, err)
			p.mu.Lock()
			for key, run := range p.runs {
				run.Status, run.Reason = "failed", "active_time_expired"
				run.Version++
				p.runs[key] = run
			}
			p.mu.Unlock()
			f.H.await(t, taskID, "failed")
			f.H.awaitReplies(t, 2)
			p.mu.Lock()
			notice := p.sent[len(p.sent)-1].Text
			p.mu.Unlock()
			if language == "ru" {
				require.Contains(t, notice, "Действия могли уже выполниться")
				require.Contains(t, notice, "Проверь их статус перед повтором")
			} else {
				require.Contains(t, notice, "Actions may already have completed")
				require.Contains(t, notice, "Check their status before retrying")
			}

			f.H.stop(t)
			f.H.start(t)
			require.Equal(t, taskID, acceptMassageContextText(t, f.H, 97500, "Continue my massage booking"))
			f.H.await(t, taskID, "failed")
			f.H.stop(t)
			retained, err := f.H.store.OwnMassageBooking(t.Context(), owner, f.Event.ID, booking.ID)
			require.NoError(t, err)
			require.Equal(t, "booked", retained.State)
			require.Equal(t, booking.Version, retained.Version)
			receipt, err := f.H.store.OwnMassageOperationReceipt(t.Context(), owner, operation)
			require.NoError(t, err)
			require.Equal(t, booking.ID, receipt.BookingID)
			require.Equal(t, "booked", receipt.Result.State)
			var count int
			require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
				"SELECT count(*) FROM shuttle_massage_operations WHERE booking_id=$1", booking.ID).Scan(&count))
			require.Equal(t, 1, count)
			p.mu.Lock()
			runs, replies := len(p.runs), len(p.sent)
			p.mu.Unlock()
			require.Equal(t, 1, runs)
			require.Equal(t, 2, replies)
		})
	}
}
