package task

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const pairExpectationContextMarker = "\nIncoming pair invitations (untrusted pending records, not consent or " +
	"current requests): "

type pairExpectation struct {
	db.PairInvitationView

	ExpiredAtObservation bool `json:"expired_at_observation"`
}

type pairExpectations struct {
	ObservedAt  time.Time         `json:"observed_at"`
	Invitations []pairExpectation `json:"invitations"`
	HasMore     bool              `json:"has_more"`
	Next        *uuid.UUID        `json:"next,omitempty"`
}

// Preserve the invitation list's pending-record semantics, including elapsed
// records. Only the existing consent write can establish current eligibility.
func (s *Service) pairExpectationPrompt(ctx context.Context, scope db.ProfileScope) ([]byte, error) {
	const pageLimit = 5
	result := pairExpectations{ObservedAt: time.Now().UTC(), Invitations: []pairExpectation{}}
	rows, err := s.store.PairInvitations(ctx, db.PaymentScope{
		BotID: scope.BotID, Organization: scope.Organization,
		UserID: scope.UserID, Subject: scope.Subject,
	}, db.PairInvitationList{Direction: "incoming", State: pairNoticePending, Limit: pageLimit + 1})
	if err != nil {
		return nil, err
	}
	return pairExpectationPage(result, rows)
}

func pairExpectationPage(result pairExpectations, rows []db.PairInvitationView) ([]byte, error) {
	const pageLimit = 5
	const pageBytes = 32 << 10
	const displayNameRunes = 256
	encodedBytes := 0
	for _, row := range rows {
		location, zoneErr := time.LoadLocation(row.Timezone)
		if zoneErr != nil {
			return nil, zoneErr
		}
		row.StartsAt, row.EndsAt = row.StartsAt.In(location), row.EndsAt.In(location)
		row.Invitation.ExpiresAt = row.Invitation.ExpiresAt.In(location)
		name := []rune(row.InviterName)
		if len(name) > displayNameRunes {
			row.InviterName = string(name[:displayNameRunes])
		}
		item := pairExpectation{PairInvitationView: row,
			ExpiredAtObservation: !row.Invitation.ExpiresAt.After(result.ObservedAt)}
		raw, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if len(result.Invitations) == pageLimit || encodedBytes+len(raw) > pageBytes {
			if len(result.Invitations) == 0 {
				return nil, errors.New("pair invitation context exceeds page limit")
			}
			result.HasMore = true
			last := result.Invitations[len(result.Invitations)-1].Invitation.ID
			result.Next = &last
			break
		}
		result.Invitations = append(result.Invitations, item)
		encodedBytes += len(raw)
	}
	return json.Marshal(result)
}
