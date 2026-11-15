package db

import (
	"context"
	"strconv"

	"github.com/google/uuid"
)

type CommunicationReply struct {
	AuthorID uuid.UUID `json:"author_id"`
	EventID  uuid.UUID `json:"event_id"`
}

// CommunicationReplyContext resolves only a message delivered to this account.
// It returns routing metadata, never another recipient's personalized text.
func (s *Store) CommunicationReplyContext(
	ctx context.Context,
	scope ProfileScope,
	messageID int64,
) (*CommunicationReply, error) {
	var reply CommunicationReply
	err := s.pool.QueryRow(ctx, `
SELECT c.author_id,coalesce(c.event_id,'00000000-0000-0000-0000-000000000000'::uuid)
 FROM shuttle_communication_deliveries d JOIN shuttle_communications c ON
c.id=d.communication_id
 JOIN shuttle_users u ON u.id=d.recipient_id WHERE d.kind='message' AND
d.state='delivered'
 AND u.id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3 AND u.zitadel_subject=$4 AND NOT
u.revoked
 AND d.receipt->>'message_id'=$5 AND d.receipt->>'chat_id'=u.tg_user_id::text LIMIT 1
`, scope.UserID, scope.BotID, scope.Organization, scope.Subject, strconv.FormatInt(messageID, 10)).
		Scan(&reply.AuthorID, &reply.EventID)
	if err != nil {
		return nil, err
	}
	return &reply, nil
}
