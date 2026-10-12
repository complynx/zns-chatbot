-- name: GetTask :one
SELECT * FROM shuttle_tasks WHERE id=$1;
-- name: FindInbox :one
SELECT task_id FROM shuttle_inbox WHERE bot_id=$1 AND update_id=$2;
-- name: UpsertUser :one
INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile) VALUES($1,$2,$3,$4) ON CONFLICT(bot_id,tg_user_id) DO UPDATE SET profile=EXCLUDED.profile RETURNING *;
-- name: EnsureConversation :one
INSERT INTO shuttle_conversations(id,user_id,chat_id) VALUES($1,$2,$3) ON CONFLICT(user_id,chat_id) DO UPDATE SET chat_id=EXCLUDED.chat_id RETURNING *;
-- name: NextSequence :one
UPDATE shuttle_conversations SET next_seq=next_seq+1 WHERE id=$1 RETURNING next_seq;
-- name: InsertTask :one
INSERT INTO shuttle_tasks(id,user_id,conversation_id,seq,request) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: InsertInbox :exec
INSERT INTO shuttle_inbox(bot_id,update_id,task_id,payload) VALUES($1,$2,$3,$4);
-- name: LockConversation :one
SELECT * FROM shuttle_conversations WHERE id=$1 FOR UPDATE;
-- name: SetRoute :one
UPDATE shuttle_tasks SET route=$2,target_run_id=$3,question_id=$4,target_task_id=$5,version=version+1 WHERE id=$1 AND route='' RETURNING *;
-- name: AdvanceRouting :exec
UPDATE shuttle_conversations SET routed_seq=$2, active_task_id=$3 WHERE id=$1;
-- name: ClaimTask :one
UPDATE shuttle_tasks SET fence=fence+1,lease_owner=$2,lease_until=now()+($3::bigint * interval '1 millisecond') WHERE id=$1 AND (lease_until IS NULL OR lease_until<=now()) RETURNING *;
-- name: SaveTask :one
UPDATE shuttle_tasks SET state=$4,run_id=$5,auth_session_id=$6,question_id=$7,snapshot=$8,version=version+1 WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>now() RETURNING *;
-- name: ReleaseTask :execrows
UPDATE shuttle_tasks SET lease_owner='',lease_until=NULL WHERE id=$1 AND lease_owner=$2 AND fence=$3;
-- name: UpdateConversationRun :exec
UPDATE shuttle_conversations SET active_run_id=$3,awaiting_question_id=$4 WHERE id=$1 AND active_task_id=$2;
-- name: ClearConversationRun :exec
UPDATE shuttle_conversations SET active_run_id='',awaiting_question_id='',active_task_id=NULL WHERE id=$1 AND active_task_id=$2;
-- name: InsertOutbox :exec
INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(dedup_key) DO NOTHING;
-- name: ListOutbox :many
SELECT * FROM shuttle_outbox WHERE task_id=$1 ORDER BY chunk_seq;
-- name: ReserveDelivery :one
UPDATE shuttle_outbox SET state='send_inflight',attempt_id=$2 WHERE shuttle_outbox.id=$1 AND shuttle_outbox.state IN ('pending','retryable') AND NOT EXISTS(SELECT 1 FROM shuttle_outbox earlier WHERE earlier.task_id=shuttle_outbox.task_id AND earlier.chunk_seq<shuttle_outbox.chunk_seq AND earlier.state NOT IN ('delivered','delivery_blocked')) RETURNING shuttle_outbox.*;
-- name: BlockPendingDelivery :exec
UPDATE shuttle_outbox SET state='delivery_blocked' WHERE id=$1 AND state IN ('pending','retryable');
-- name: FinishDelivery :one
UPDATE shuttle_outbox SET state=$3,receipt=$4 WHERE id=$1 AND attempt_id=$2 AND state='send_inflight' RETURNING *;
-- name: RecoverUnknownDeliveries :execrows
UPDATE shuttle_outbox SET state='delivery_unknown' WHERE state='send_inflight';
-- name: GetUser :one
SELECT * FROM shuttle_users WHERE id=$1;
-- name: BindIdentity :one
UPDATE shuttle_users SET zitadel_subject=$2,zitadel_org=$3 WHERE id=$1 AND (zitadel_subject='' OR zitadel_subject=$2) AND (zitadel_org='' OR zitadel_org=$3) RETURNING *;
-- name: RevokeUser :execrows
UPDATE shuttle_users SET revoked=$2 WHERE id=$1;
-- name: GetSettings :one
SELECT * FROM shuttle_settings WHERE id=true;
-- name: CASSettings :one
UPDATE shuttle_settings SET version=version+1,document=$2 WHERE id=true AND version=$1 RETURNING *;
-- name: AuditSettings :exec
INSERT INTO shuttle_settings_audit(actor,version,old_document,new_document) VALUES($1,$2,$3,$4);

-- name: GetUserBySubject :one
SELECT * FROM shuttle_users WHERE zitadel_subject=$1 ORDER BY bot_id,tg_user_id LIMIT 1;




-- name: AuditIdentity :exec
INSERT INTO shuttle_identity_audit(user_id,actor,revoked) VALUES($1,$2,$3);

-- name: ControlReady :one
SELECT NOT EXISTS (
 SELECT 1 FROM shuttle_tasks earlier
 JOIN shuttle_tasks current_task ON current_task.id=$1
 WHERE earlier.conversation_id=current_task.conversation_id
 AND earlier.target_task_id=current_task.target_task_id
 AND earlier.seq<current_task.seq
 AND earlier.route IN ('input','cancel','approval')
 AND earlier.state NOT IN ('completed','failed','cancelled','delivery_unknown','delivery_failed','closed')
) AS ready;
-- name: BlockDeliverySuffix :exec
UPDATE shuttle_outbox SET state='delivery_blocked'
WHERE task_id=$1 AND chunk_seq>$2 AND state IN ('pending','retryable');
-- name: PromoteTask :one
UPDATE shuttle_tasks SET route='new_run',target_task_id='00000000-0000-0000-0000-000000000000',target_run_id='',run_id='',auth_session_id='',question_id='',snapshot='{}',state='accepted',version=version+1
WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>now() AND route='input' AND request->>'kind'='message'
RETURNING *;
-- name: ActivatePromotedTask :exec
UPDATE shuttle_conversations SET active_task_id=$2,active_run_id='',awaiting_question_id='' WHERE id=$1;
-- name: HasEarlierPromotableMessage :one
SELECT EXISTS (
 SELECT 1 FROM shuttle_tasks earlier
 JOIN shuttle_tasks target ON target.id=earlier.target_task_id
 WHERE earlier.conversation_id=$1 AND earlier.seq<$2
 AND earlier.request->>'kind'='message' AND earlier.route='input'
 AND earlier.state NOT IN ('completed','failed','cancelled','delivery_unknown','delivery_failed','closed')
 AND target.state IN ('completed','failed','cancelled','delivery_unknown','delivery_failed','closed')
) AS has_earlier;

-- name: GetAdminTask :one
SELECT t.* FROM shuttle_tasks t JOIN shuttle_users u ON u.id=t.user_id
WHERE t.id=sqlc.arg(id) AND u.bot_id=sqlc.arg(bot_id)
AND (u.zitadel_org='' OR u.zitadel_org=sqlc.arg(org));
-- name: LockAdminTask :one
SELECT t.* FROM shuttle_tasks t JOIN shuttle_users u ON u.id=t.user_id
WHERE t.id=sqlc.arg(id) AND u.bot_id=sqlc.arg(bot_id)
AND (u.zitadel_org='' OR u.zitadel_org=sqlc.arg(org)) FOR UPDATE OF t;
-- name: ListAdminTasks :many
SELECT t.* FROM shuttle_tasks t JOIN shuttle_users u ON u.id=t.user_id
WHERE u.bot_id=sqlc.arg(bot_id) AND (u.zitadel_org='' OR u.zitadel_org=sqlc.arg(org))
AND (sqlc.arg(state)::text='' OR t.state=sqlc.arg(state))
AND (sqlc.arg(after_id)::uuid='00000000-0000-0000-0000-000000000000' OR t.id>sqlc.arg(after_id))
ORDER BY t.id LIMIT sqlc.arg(page_limit);
-- name: GetTaskResolution :one
SELECT * FROM shuttle_task_resolutions WHERE task_id=$1 AND operation_id=$2;
-- name: ListTaskResolutions :many
SELECT * FROM shuttle_task_resolutions WHERE task_id=$1 ORDER BY created_at,operation_id;
-- name: CloseUncertainTask :one
UPDATE shuttle_tasks SET state='closed',version=version+1,fence=fence+1,lease_owner='',lease_until=NULL
WHERE id=$1 AND version=$2 AND (lease_until IS NULL OR lease_until<=now()) RETURNING *;
-- name: InsertTaskResolution :one
INSERT INTO shuttle_task_resolutions(task_id,operation_id,actor,expected_version,resulting_version,reason,previous_state)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;
