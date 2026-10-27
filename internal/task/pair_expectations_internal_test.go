package task

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPairExpectationPageKeepsElapsedRecordsAndCursor(t *testing.T) {
	t.Parallel()
	observed := time.Date(2026, time.March, 29, 1, 30, 0, 0, time.UTC)
	rows := make([]db.PairInvitationView, 6)
	for i := range rows {
		rows[i] = db.PairInvitationView{Invitation: registrations.Invitation{
			ID: uuid.New(), ExpiresAt: observed.Add(time.Duration(i-1) * time.Minute)},
			Timezone: "Europe/Berlin", InviterName: strings.Repeat("Я", 300)}
	}
	raw, err := pairExpectationPage(pairExpectations{ObservedAt: observed, Invitations: []pairExpectation{}}, rows)
	require.NoError(t, err)
	var page pairExpectations
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Invitations, 5)
	require.True(t, page.HasMore)
	require.Equal(t, rows[4].Invitation.ID, *page.Next)
	require.True(t, page.Invitations[0].ExpiredAtObservation)
	require.True(t, page.Invitations[1].ExpiredAtObservation, "Expiry equality is elapsed, not consent")
	require.False(t, page.Invitations[2].ExpiredAtObservation)
	require.Equal(t, strings.Repeat("Я", 256), page.Invitations[0].InviterName)
	require.Equal(t, "2026-03-29T03:30:00+02:00", page.Invitations[1].Invitation.ExpiresAt.Format(time.RFC3339))
	require.Less(t, len(raw), 32<<10)
}

func TestPairExpectationPageStopsBeforeJSONBudgetWithoutSkipping(t *testing.T) {
	t.Parallel()
	observed := time.Now().UTC()
	row := db.PairInvitationView{Invitation: registrations.Invitation{ID: uuid.New(), ExpiresAt: observed},
		Timezone: "Europe/Minsk", EventName: strings.Repeat("<", 3000)}
	rows := []db.PairInvitationView{row, row}
	rows[1].Invitation.ID = uuid.New()
	raw, err := pairExpectationPage(pairExpectations{ObservedAt: observed, Invitations: []pairExpectation{}}, rows)
	require.NoError(t, err)
	var page pairExpectations
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Invitations, 1)
	require.True(t, page.HasMore)
	require.Equal(t, row.Invitation.ID, *page.Next)
	require.Less(t, len(raw), 32<<10)
}
