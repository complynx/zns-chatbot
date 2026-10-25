package db

import "context"

const TypingPageSize = 64

// The candidate set uses unresolved task/outbox indexes. Waiting for a person or
// recovery of an uncertain send is not processing. Queued sends remain active
// after their task has completed, including separately queued native avatars.
const typingCandidates = `WITH busy AS (
 SELECT t.user_id,c.chat_id FROM shuttle_tasks t
 JOIN shuttle_conversations c ON c.id=t.conversation_id AND c.user_id=t.user_id
 WHERE (t.state IN ('accepted','creating_run','running')
 OR (t.state='awaiting_service' AND t.run_id=''
 AND jsonb_typeof(t.request->'symbols')='array' AND t.request->'symbols'<>'[]'::jsonb
 AND statement_timestamp()<t.created_at+interval '6 minutes')
 OR (t.state='awaiting_authorization' AND t.request->'native_avatar'='true'::jsonb
 AND t.lease_until>statement_timestamp()))
 AND (t.lease_until IS NULL OR t.lease_until>statement_timestamp())
 UNION
 SELECT t.user_id,c.chat_id FROM shuttle_outbox o
 JOIN shuttle_tasks t ON t.id=o.task_id
 JOIN shuttle_conversations c ON c.id=t.conversation_id AND c.user_id=t.user_id
 WHERE o.state IN ('pending','retryable','send_inflight')
 AND t.state NOT IN ('delivery_unknown','auth_unknown','closed')
 AND NOT EXISTS(SELECT 1 FROM shuttle_outbox prior WHERE prior.task_id=o.task_id
 AND prior.chunk_seq<o.chunk_seq AND prior.state IN ('delivery_unknown','delivery_failed'))
 UNION
 SELECT user_id,telegram_user_id FROM shuttle_avatar_deliveries
 WHERE state IN ('pending','retryable','send_inflight')
), eligible AS (
 SELECT DISTINCT b.chat_id FROM busy b JOIN shuttle_users u ON u.id=b.user_id
 WHERE u.bot_id=$1 AND NOT u.revoked AND u.tg_user_id>0 AND b.chat_id=u.tg_user_id
 AND (u.zitadel_org=$2 OR (u.zitadel_org='' AND u.zitadel_subject=''))
) `

// TypingChats returns a bounded keyset page, never message contents or identity
// credentials. Unbound users are limited to this bot's trusted private intake.
func (s *Store) TypingChats(ctx context.Context, botID int64, organization string, after int64) ([]int64, error) {
	rows, err := s.pool.Query(
		ctx,
		typingCandidates+`SELECT chat_id FROM eligible WHERE chat_id>$3 ORDER BY chat_id LIMIT 64`,
		botID,
		organization,
		after,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		chats = append(chats, id)
	}
	return chats, rows.Err()
}

// NeedsTyping rechecks a candidate immediately before the cosmetic API call.
func (s *Store) NeedsTyping(ctx context.Context, botID int64, organization string, chatID int64) (bool, error) {
	var needed bool
	err := s.pool.QueryRow(
		ctx,
		typingCandidates+`SELECT EXISTS(SELECT 1 FROM eligible WHERE chat_id=$3)`,
		botID,
		organization,
		chatID,
	).
		Scan(&needed)
	return needed, err
}
