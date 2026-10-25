package db_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestTypingTracksProcessingAndQueuedDeliveriesWithoutWaitingOrUnknown(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	task, _, err := s.Intake(
		ctx,
		db.IntakeRequest{
			BotID:          1,
			UpdateID:       1,
			TelegramUserID: 42,
			ChatID:         42,
			Payload:        json.RawMessage(`{"kind":"message"}`),
		},
		nil,
	)
	require.NoError(t, err)
	check := func(want bool) {
		t.Helper()
		got, readErr := s.NeedsTyping(ctx, 1, "org", 42)
		require.NoError(t, readErr)
		require.Equal(t, want, got)
		page, readErr := s.TypingChats(ctx, 1, "org", 0)
		require.NoError(t, readErr)
		if want {
			require.Equal(t, []int64{42}, page)
		} else {
			require.Empty(t, page)
		}
	}
	check(true)
	for _, state := range []string{
		"awaiting_user",
		"awaiting_service",
		"auth_unknown",
		"delivery_unknown",
		"failed",
		"cancelled",
		"completed",
	} {
		_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET state=$2 WHERE id=$1`, task.ID, state)
		require.NoError(t, err)
		check(false)
	}
	// Cold symbol descriptions are processing until their original intake deadline.
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET state='awaiting_service',
request=jsonb_set(request,'{symbols}','[{"kind":"emoji","id":"1","offset":0,"length":2}]') WHERE id=$1`, task.ID)
	require.NoError(t, err)
	check(true)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET created_at=now()-interval '7 minutes' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	check(false)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET created_at=now(),run_id='known-run' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	check(false)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET state='completed' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	outbox := uuid.New()
	_, err = s.Pool().
		Exec(ctx, "INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload) VALUES($1,$2,"+
			"'typing-test',0,'{}')", outbox, task.ID)
	require.NoError(t, err)
	check(true) // A completed task still owes its final reply.
	for _, state := range []string{"retryable", "send_inflight", "delivery_unknown", "delivery_failed", "delivered"} {
		_, err = s.Pool().
			Exec(ctx, `UPDATE shuttle_outbox SET state=$2,attempt_id=$3 WHERE id=$1`, outbox, state, uuid.New())
		require.NoError(t, err)
		check(state == "retryable" || state == "send_inflight")
	}
	draft, artifact, delivery := uuid.New(), uuid.New(), uuid.New()
	hash := make([]byte, 32)
	_, err = s.Pool().
		Exec(ctx, "INSERT INTO shuttle_avatar_drafts(id,user_id,original,source_sha256,mime_type,"+
			"frame_sha256,transform,version,source_size_bytes) VALUES($1,$2,$3,$4,'image/jpeg',$4,"+
			"'{}',1,1)", draft, task.UserID, []byte{1}, hash)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(
			ctx,
			"INSERT INTO shuttle_avatars(id,user_id,operation_id,request_hash,receipt,jpeg,"+
				"draft_id,draft_version,native) VALUES($1,$2,$3,$4,'{}',$5,$6,1,true)",
			artifact,
			task.UserID,
			uuid.New(),
			hash,
			[]byte{1},
			draft,
		)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(ctx, "INSERT INTO shuttle_avatar_deliveries(id,artifact_id,user_id,telegram_user_id) "+
			"VALUES($1,$2,$3,42)", delivery, artifact, task.UserID)
	require.NoError(t, err)
	check(true) // No running Loom task is required for native delivery.
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_avatar_deliveries SET state='delivery_unknown' WHERE id=$1`, delivery)
	require.NoError(t, err)
	check(false)
	_, err = s.Pool().
		Exec(ctx, "UPDATE shuttle_tasks SET state='running',lease_until=statement_timestamp()-interval "+
			"'1 second' WHERE id=$1", task.ID)
	require.NoError(t, err)
	check(false)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET lease_until=NULL WHERE id=$1`, task.ID)
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_users SET revoked=true WHERE id=$1`, task.UserID)
	require.NoError(t, err)
	check(false)
}
