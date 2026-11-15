package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
)

type CommunicationAudienceQuery struct {
	EventID           uuid.UUID `json:"event_id,omitempty"`
	After             uuid.UUID `json:"after,omitempty"`
	RegistrationState string    `json:"registration_state,omitempty" enum:"waitlisted,assigned"`
	Role              string    `json:"role,omitempty" enum:"leader,follower"`
	Contacts          bool      `json:"contacts,omitempty"`
}
type CommunicationAudience struct {
	Users []BasicPerson `json:"users"`
	Next  *uuid.UUID    `json:"next,omitempty"`
}

// CommunicationAudience provides basic staff slices. The agent can further filter
// the explicit list using existing scoped domain reads before preparing a message.
func (s *Store) CommunicationAudience(
	ctx context.Context,
	scope PaymentScope,
	a CommunicationAudienceQuery,
) (CommunicationAudience, error) {
	out := CommunicationAudience{Users: []BasicPerson{}}
	if (a.RegistrationState != "" && a.RegistrationState != "waitlisted" && a.RegistrationState != "assigned") ||
		(a.Role != "" && a.Role != "leader" && a.Role != "follower") ||
		((a.Role != "" || a.RegistrationState != "" || a.Contacts) && a.EventID == uuid.Nil) {
		return out, broadcasts.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	mode := peopleScopeAll
	if a.Contacts {
		if err = lockCommunicationActor(ctx, tx, scope, a.EventID); err != nil {
			return out, err
		}
	} else {
		mode, err = s.peopleReadScope(ctx, tx, scope, a.EventID)
		if err != nil {
			return out, err
		}
		if mode == peopleScopePublic {
			return out, ErrPeopleDenied
		}
	}
	rows, err := tx.Query(
		ctx,
		`
SELECT
`+basicPersonSQL+` FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 WHERE `+peopleAccessSQL+` AND NOT u.revoked AND u.zitadel_subject<>'' AND u.id>$7
 AND ($8='' OR EXISTS(SELECT 1 FROM shuttle_registrations r WHERE r.event_id=$4 AND
r.user_id=u.id AND r.state=$8))
 AND ($9='' OR EXISTS(SELECT 1 FROM shuttle_registrations r WHERE r.event_id=$4 AND
r.user_id=u.id AND r.role=$9))
 AND (NOT $10 OR EXISTS(SELECT 1 FROM shuttle_event_authorities a JOIN shuttle_registrations r
ON r.event_id=a.event_id
 WHERE a.event_id=$4 AND a.user_id=u.id AND a.role IN ('administrator','ambassador') AND r.user_id=$5))
 ORDER BY u.id LIMIT $11`,
		scope.BotID,
		scope.Organization,
		mode,
		optionalUUID(a.EventID),
		scope.UserID,
		nil,
		a.After,
		a.RegistrationState,
		a.Role,
		a.Contacts,
		peopleMaxPage+1,
	)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p BasicPerson
		var tg int64
		if err = rows.Scan(
			&p.UserID,
			&p.FullName,
			&p.Username,
			&tg,
			&p.Gender,
			&p.Pronouns,
			&p.GenderSource,
			&p.PronounsSource,
		); err != nil {
			return out, err
		}
		if len(out.Users) == peopleMaxPage {
			last := out.Users[peopleMaxPage-1].UserID
			out.Next = &last
			break
		}
		p.ProfileURL = fmt.Sprintf("tg://user?id=%d", tg)
		out.Users = append(out.Users, p)
	}
	return out, rows.Err()
}
