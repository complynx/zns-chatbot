package db

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// PairAssignmentParticipant is bounded display context, never legal identity or consent.
type PairAssignmentParticipant struct {
	UserID         uuid.UUID `json:"user_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	DisplayName    string    `json:"display_name"`
}

func pairAssignmentParticipant(ctx context.Context, tx pgx.Tx, q *sqlc.Queries,
	id uuid.UUID,
) (PairAssignmentParticipant, bool, error) {
	user, err := q.GetUser(ctx, id)
	if err != nil {
		return PairAssignmentParticipant{}, false, err
	}
	if user.Revoked {
		return PairAssignmentParticipant{UserID: id, TelegramUserID: user.TgUserID}, false, nil
	}
	profile, err := readProfile(ctx, tx, id)
	if err != nil {
		return PairAssignmentParticipant{}, false, err
	}
	return PairAssignmentParticipant{
		UserID:         id,
		TelegramUserID: user.TgUserID,
		DisplayName:    strings.TrimSpace(profile.FullName),
	}, true, nil
}

// Identity metadata comes from the original pair membership, independent of the
// current candidate queue. It is display context, not part of the frozen terms.
func enrichPairAssignmentView(ctx context.Context, tx pgx.Tx, q *sqlc.Queries,
	view *PairAssignmentProposalView,
) error {
	args := view.Preparation.Arguments
	pair, err := q.GetPair(ctx, sqlc.GetPairParams{ID: args.PairID, EventID: args.EventID})
	if err != nil {
		return err
	}
	participant := func(registration uuid.UUID) (PairAssignmentParticipant, error) {
		entry, e := q.GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: registration, EventID: args.EventID})
		if e != nil {
			return PairAssignmentParticipant{}, e
		}
		identity, _, e := pairAssignmentParticipant(ctx, tx, q, entry.UserID)
		return identity, e
	}
	leader, err := participant(pair.LeaderRegistrationID)
	if err != nil {
		return err
	}
	follower, err := participant(pair.FollowerRegistrationID)
	if err != nil {
		return err
	}
	view.Leader, view.Follower = &leader, &follower
	return nil
}
