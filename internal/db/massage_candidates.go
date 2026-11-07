package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/massage"
)

type MassageCandidateArguments struct {
	EventID              uuid.UUID                      `json:"event_id"`
	ExpectedEventVersion int64                          `json:"expected_event_version" minimum:"1"`
	ExpectedVersion      int64                          `json:"expected_version"       minimum:"1"`
	Query                massage.ScheduleCandidateQuery `json:"query"`
}

type MassageCandidateView struct {
	massage.ScheduleCandidatePage

	EventID      uuid.UUID `json:"event_id"`
	EventVersion int64     `json:"event_version"`
	Version      int64     `json:"version"`
}

// OwnMassageCandidates computes after releasing the single authorized snapshot's
// connection. These are schedule starts, not reservation availability.
func (s *Store) OwnMassageCandidates(
	ctx context.Context, scope ProfileScope, args MassageCandidateArguments,
) (MassageCandidateView, error) {
	snapshot, err := s.OwnMassageSchedule(ctx, scope, args.EventID)
	if err != nil {
		return MassageCandidateView{}, err
	}
	if args.ExpectedEventVersion != snapshot.EventVersion || args.ExpectedVersion != snapshot.Version {
		return MassageCandidateView{}, ErrConflict
	}
	page, err := massage.CandidateStarts(snapshot.Document, args.Query, snapshot.ObservedAt, snapshot.Timezone)
	if err != nil {
		return MassageCandidateView{}, err
	}
	return MassageCandidateView{ScheduleCandidatePage: page, EventID: snapshot.EventID,
		EventVersion: snapshot.EventVersion, Version: snapshot.Version}, nil
}
