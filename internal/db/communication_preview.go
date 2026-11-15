package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

const communicationPreviewLimit = 5

type CommunicationContact struct {
	Name     string
	Username string
}

type CommunicationPreview struct {
	EventName string
	Contacts  []CommunicationContact
}

// CommunicationPreview checks current access before reading names; it stores no copied profiles.
func (s *Store) CommunicationPreview(
	ctx context.Context,
	scope PaymentScope,
	c Communication,
	targetAdmin bool,
) (CommunicationPreview, error) {
	out := CommunicationPreview{Contacts: []CommunicationContact{}}
	if scope.UserID != c.AuthorID {
		return out, ErrPeopleDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockCommunicationActor(ctx, tx, scope, c.Arguments.EventID); err != nil {
		return out, err
	}
	if err = communicationRecipientsAccess(
		ctx,
		tx,
		scope,
		c.Arguments.EventID,
		c.Arguments.Recipients,
		c.Category == communicationPersonal,
		targetAdmin,
	); err != nil {
		return out, err
	}
	if c.Arguments.EventID != uuid.Nil {
		if err = tx.QueryRow(ctx, `SELECT name FROM shuttle_events WHERE id=$1`, c.Arguments.EventID).
			Scan(&out.EventName); err != nil {
			return out, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT `+personNameSQL+`,`+personUsernameSQL+`,u.tg_user_id
 FROM unnest($3::uuid[]) WITH ORDINALITY selected(id,ord)
 JOIN shuttle_users u ON u.id=selected.id LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 WHERE u.bot_id=$1 AND u.zitadel_org=$2 ORDER BY selected.ord LIMIT $4`,
		scope.BotID, scope.Organization, c.Arguments.Recipients, communicationPreviewLimit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item CommunicationContact
		var telegramID int64
		if err = rows.Scan(&item.Name, &item.Username, &telegramID); err != nil {
			return out, err
		}
		item.Name = communicationName(item.Name, item.Username, telegramID)
		out.Contacts = append(out.Contacts, item)
	}
	return out, rows.Err()
}

func communicationName(name, username string, telegramID int64) string {
	if name != "" {
		return name
	}
	if username != "" {
		return "@" + username
	}
	return fmt.Sprintf("Telegram #%d", telegramID)
}
