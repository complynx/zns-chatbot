-- name: GetTask :one
SELECT * FROM shuttle_tasks WHERE id=$1;
-- name: EnsureTelegramUser :one
INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile) VALUES($1,$2,$3,$4)
ON CONFLICT(bot_id,tg_user_id) DO UPDATE SET tg_user_id=shuttle_users.tg_user_id RETURNING *;
-- name: LockTelegramUser :one
SELECT * FROM shuttle_users WHERE id=$1 AND bot_id=$2 FOR UPDATE;
-- name: InsertMiniAppSession :exec
INSERT INTO shuttle_miniapp_sessions(id,bot_id,user_id,proof_hash,nonce_hash,key_generation,subject,organization,auth_date,pending_until,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(bot_id,proof_hash) DO NOTHING;
-- name: LockMiniAppProof :one
SELECT * FROM shuttle_miniapp_sessions WHERE bot_id=$1 AND proof_hash=$2 FOR UPDATE;
-- name: GetMiniAppSession :one
SELECT * FROM shuttle_miniapp_sessions WHERE id=$1;
-- name: LockMiniAppSession :one
SELECT * FROM shuttle_miniapp_sessions WHERE id=$1 FOR UPDATE;
-- name: FindMiniAppBearer :one
SELECT * FROM shuttle_miniapp_sessions WHERE bot_id=$1 AND bearer_hash=$2;
-- name: ActivateMiniAppSession :one
UPDATE shuttle_miniapp_sessions SET state='active',subject=$2,organization=$3,bearer_hash=$4
WHERE id=$1 AND expires_at>clock_timestamp()
AND (state='active' OR (state='pending' AND pending_until>clock_timestamp())) RETURNING *;
-- name: RevokeMiniAppSession :exec
UPDATE shuttle_miniapp_sessions SET state='revoked' WHERE id=$1;
-- name: PruneMiniAppSessions :execrows
DELETE FROM shuttle_miniapp_sessions target WHERE target.id IN (
 SELECT expired.id FROM shuttle_miniapp_sessions expired WHERE expired.bot_id=$1
 AND expired.auth_date<=clock_timestamp()-interval '24 hours'
 AND expired.expires_at<=clock_timestamp() AND expired.pending_until<=clock_timestamp()
 ORDER BY expired.auth_date LIMIT 100 FOR UPDATE SKIP LOCKED);
-- name: FindInbox :one
SELECT task_id FROM shuttle_inbox WHERE bot_id=$1 AND update_id=$2;
-- name: TaskMetrics :many
SELECT (CASE WHEN state IN (
 'accepted','creating_run','running','awaiting_service','awaiting_authorization',
 'auth_reauthorizing','awaiting_user','auth_unknown','delivery_unknown'
) THEN state ELSE 'other' END)::text AS metric_state,
count(*)::bigint AS task_count,
coalesce(extract(epoch FROM (statement_timestamp()-min(created_at))),0)::double precision AS accepted_age_seconds
FROM shuttle_tasks
WHERE state NOT IN ('completed','failed','cancelled','delivery_failed','closed')
GROUP BY metric_state ORDER BY metric_state;
-- name: OutboxMetrics :many
SELECT (CASE WHEN state IN ('pending','retryable','send_inflight','delivery_unknown')
 THEN state ELSE 'other' END)::text AS metric_state,
count(*)::bigint AS chunk_count,
coalesce(extract(epoch FROM (statement_timestamp()-min(created_at))),0)::double precision AS created_age_seconds
FROM shuttle_outbox
WHERE state NOT IN ('delivered','delivery_blocked','delivery_failed')
GROUP BY metric_state ORDER BY metric_state;
-- name: UpsertUser :one
INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile) VALUES($1,$2,$3,$4) ON CONFLICT(bot_id,tg_user_id) DO UPDATE SET profile=EXCLUDED.profile RETURNING *;
-- name: EnsureConversation :one
INSERT INTO shuttle_conversations(id,user_id,chat_id) VALUES($1,$2,$3) ON CONFLICT(user_id,chat_id) DO UPDATE SET chat_id=EXCLUDED.chat_id RETURNING *;
-- name: NextSequence :one
UPDATE shuttle_conversations SET next_seq=next_seq+1 WHERE id=$1 RETURNING next_seq;
-- name: InsertTask :one
INSERT INTO shuttle_tasks(id,user_id,conversation_id,seq,request,response_started_at) VALUES($1,$2,$3,$4,$5,clock_timestamp()) RETURNING *;
-- name: InsertInbox :exec
INSERT INTO shuttle_inbox(bot_id,update_id,task_id,payload) VALUES($1,$2,$3,$4);
-- name: LockConversation :one
SELECT * FROM shuttle_conversations WHERE id=$1 FOR UPDATE;
-- name: SetRoute :one
UPDATE shuttle_tasks SET route=$2,target_run_id=$3,question_id=$4,target_task_id=$5,version=version+1 WHERE id=$1 AND route='' RETURNING *;
-- name: RoutedSequence :one
SELECT coalesce((SELECT seq-1 FROM shuttle_tasks
 WHERE conversation_id=sqlc.arg(conversation_id) AND seq>sqlc.arg(routed_seq) AND route=''
 ORDER BY seq LIMIT 1),sqlc.arg(next_seq)::bigint)::bigint;
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
-- name: DeliveryTaskID :one
SELECT task_id FROM shuttle_outbox WHERE id=$1;
-- name: LockResponseTask :exec
SELECT id FROM shuttle_tasks WHERE id=$1 FOR UPDATE;
-- name: RegisterFinalResponse :exec
UPDATE shuttle_tasks t SET response_first_chunk_seq=r.first_seq,response_last_chunk_seq=r.last_seq
FROM (SELECT min(chunk_seq) AS first_seq,max(chunk_seq) AS last_seq,count(*) AS chunks
 FROM shuttle_outbox WHERE task_id=sqlc.arg(task_id) AND dedup_key=ANY(sqlc.arg(outbox_keys)::text[])) r
WHERE t.id=sqlc.arg(task_id) AND t.route='new_run' AND t.state='completed' AND t.run_id<>''
AND t.response_started_at IS NOT NULL AND t.response_first_chunk_seq IS NULL
AND r.first_seq>=0 AND r.chunks>0 AND r.chunks=cardinality(sqlc.arg(outbox_keys)::text[])
AND r.last_seq::bigint-r.first_seq::bigint+1=r.chunks;
-- name: CompleteFinalResponse :one
UPDATE shuttle_tasks t SET response_delivered_at=clock_timestamp()
WHERE t.id=sqlc.arg(task_id) AND t.route='new_run' AND t.state='completed' AND t.run_id<>''
AND t.response_started_at IS NOT NULL AND t.response_first_chunk_seq IS NOT NULL
AND t.response_last_chunk_seq IS NOT NULL AND t.response_delivered_at IS NULL
AND isfinite(t.response_started_at)
AND sqlc.arg(chunk_seq)::integer BETWEEN t.response_first_chunk_seq AND t.response_last_chunk_seq
AND (SELECT count(*) FROM shuttle_outbox o WHERE o.task_id=t.id
 AND o.chunk_seq BETWEEN t.response_first_chunk_seq AND t.response_last_chunk_seq)
 =t.response_last_chunk_seq::bigint-t.response_first_chunk_seq::bigint+1
AND NOT EXISTS(SELECT 1 FROM shuttle_outbox o WHERE o.task_id=t.id
 AND o.chunk_seq BETWEEN t.response_first_chunk_seq AND t.response_last_chunk_seq AND o.state<>'delivered')
RETURNING extract(epoch FROM (response_delivered_at-response_started_at))::double precision AS elapsed_seconds;
-- name: RecoverUnknownDeliveries :execrows
UPDATE shuttle_outbox SET state='delivery_unknown' WHERE state='send_inflight';
-- name: GetUser :one
SELECT * FROM shuttle_users WHERE id=$1;
-- name: BindIdentity :one
UPDATE shuttle_users SET zitadel_subject=$2,zitadel_org=$3 WHERE id=$1 AND (zitadel_subject='' OR zitadel_subject=$2) AND (zitadel_org='' OR zitadel_org=$3) RETURNING *;
-- name: RevokeUser :execrows
UPDATE shuttle_users SET revoked=$2 WHERE id=$1;

-- name: GetRevocationUser :one
SELECT * FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3;
-- name: LockRevocationUsers :many
SELECT * FROM shuttle_users WHERE id=ANY(sqlc.arg(ids)::uuid[])
AND bot_id=sqlc.arg(bot_id) AND zitadel_org=sqlc.arg(organization_id)
ORDER BY id FOR UPDATE;
-- name: GetUserRevocationProposal :one
SELECT * FROM shuttle_user_revocation_proposals
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: GetUserRevocationProposalByID :one
SELECT * FROM shuttle_user_revocation_proposals
WHERE bot_id=$1 AND organization_id=$2 AND proposal_id=$3;
-- name: InsertUserRevocationProposal :exec
INSERT INTO shuttle_user_revocation_proposals
(bot_id,organization_id,actor,operation_id,proposal_id,target_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: SaveUserRevocationResult :execrows
UPDATE shuttle_user_revocation_proposals SET result=$5
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 AND result IS NULL;
-- name: GetSettings :one
SELECT * FROM shuttle_settings WHERE id=true;
-- name: LockSettings :one
SELECT * FROM shuttle_settings WHERE id=true FOR UPDATE;
-- name: CASSettings :one
UPDATE shuttle_settings SET version=version+1,document=$2 WHERE id=true AND version=$1 RETURNING *;
-- name: AuditSettings :exec
INSERT INTO shuttle_settings_audit(actor,version,old_document,new_document) VALUES($1,$2,$3,$4);

-- name: GetSettingsChangeProposal :one
SELECT * FROM shuttle_settings_change_proposals
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertSettingsChangeProposal :exec
INSERT INTO shuttle_settings_change_proposals
(bot_id,organization_id,actor,operation_id,proposal_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: SaveSettingsChangeResult :execrows
UPDATE shuttle_settings_change_proposals SET result=$5
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 AND result IS NULL;

-- name: GetUserBySubject :one
SELECT * FROM shuttle_users WHERE bot_id=$1 AND zitadel_org=$2 AND zitadel_subject=$3 ORDER BY tg_user_id LIMIT 1;

-- name: GetEventChangeProposal :one
SELECT * FROM shuttle_event_change_proposals
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertEventChangeProposal :exec
INSERT INTO shuttle_event_change_proposals
(bot_id,organization_id,actor,operation_id,proposal_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: SaveEventChangeResult :execrows
UPDATE shuttle_event_change_proposals SET result=$5
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 AND result IS NULL;




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

-- name: GetTaskClosureProposal :one
SELECT * FROM shuttle_task_closure_proposals
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertTaskClosureProposal :exec
INSERT INTO shuttle_task_closure_proposals
(bot_id,organization_id,actor,operation_id,proposal_id,task_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: SaveTaskClosureResult :execrows
UPDATE shuttle_task_closure_proposals SET result=$5
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 AND result IS NULL;

-- name: GetEvent :one
SELECT * FROM shuttle_events WHERE id=$1 AND bot_id=$2 AND organization_id=$3;
-- name: ListEvents :many
SELECT * FROM shuttle_events WHERE bot_id=$1 AND organization_id=$2 AND id>$3 ORDER BY id LIMIT $4;
-- name: CreateEvent :one
INSERT INTO shuttle_events(id,bot_id,organization_id,name,timezone,starts_at,ends_at,modules)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING RETURNING *;
-- name: UpdateEvent :one
UPDATE shuttle_events SET name=$5,timezone=$6,starts_at=$7,ends_at=$8,modules=$9,version=version+1
WHERE id=$1 AND bot_id=$2 AND organization_id=$3 AND version=$4 RETURNING *;
-- name: LockEventOperation :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text,0));
-- name: GetEventOperation :one
SELECT * FROM shuttle_event_operations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertEventOperation :exec
INSERT INTO shuttle_event_operations(bot_id,organization_id,actor,operation_id,request_hash,result)
VALUES($1,$2,$3,$4,$5,$6);

-- name: GetDeployment :one
SELECT bot_id,organization_id,issuer FROM shuttle_deployment WHERE id=true;

-- name: BindDeployment :exec
INSERT INTO shuttle_deployment(id,bot_id,organization_id,issuer) VALUES(true,$1,$2,$3) ON CONFLICT(id) DO NOTHING;

-- name: DeploymentDataMatches :one
SELECT NOT EXISTS (SELECT 1 FROM shuttle_users WHERE bot_id<>$1 OR (zitadel_org<>'' AND zitadel_org<>$2));

-- name: GetEventConfiguration :one
SELECT sqlc.embed(e),r.document AS registration_rules,d.document AS deposit_policy FROM shuttle_events e
LEFT JOIN shuttle_event_registration_rules r ON r.event_id=e.id
LEFT JOIN shuttle_event_deposit_policy d ON d.event_id=e.id
WHERE e.id=$1 AND e.bot_id=$2 AND e.organization_id=$3;

-- name: ListEventConfigurations :many
SELECT sqlc.embed(e),r.document AS registration_rules,d.document AS deposit_policy FROM shuttle_events e
LEFT JOIN shuttle_event_registration_rules r ON r.event_id=e.id
LEFT JOIN shuttle_event_deposit_policy d ON d.event_id=e.id
WHERE e.bot_id=$1 AND e.organization_id=$2 AND e.id>$3 ORDER BY e.id LIMIT $4;

-- name: SetEventRegistrationRules :exec
INSERT INTO shuttle_event_registration_rules(event_id,document) VALUES($1,$2)
ON CONFLICT(event_id) DO UPDATE SET document=EXCLUDED.document;

-- name: LockRegistrationEvent :one
SELECT * FROM shuttle_events WHERE id=$1 AND bot_id=$2 AND organization_id=$3 FOR UPDATE;
-- name: ReadRegistrationRules :one
SELECT document FROM shuttle_event_registration_rules WHERE event_id=$1;
-- name: RegistrationNow :one
SELECT clock_timestamp()::timestamptz AS current_time;
-- name: LockRegistrationUser :one
SELECT * FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3 AND NOT revoked FOR SHARE;
-- name: GetRegistration :one
SELECT * FROM shuttle_registrations WHERE event_id=$1 AND user_id=$2;
-- name: GetRegistrationByID :one
SELECT * FROM shuttle_registrations WHERE id=$1 AND event_id=$2;
-- name: CreateRegistration :one
INSERT INTO shuttle_registrations(id,event_id,user_id,role,requested_at) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: AssignRegistration :one
UPDATE shuttle_registrations SET state='assigned',version=version+1 WHERE id=$1 AND version=$2 AND state='waitlisted' RETURNING *;
-- name: CountAssignedRole :one
SELECT count(*) FROM shuttle_registrations WHERE event_id=$1 AND role=$2 AND state='assigned';
-- name: CreatePass :one
INSERT INTO shuttle_passes(id,registration_id,price_minor,currency,currency_scale,assigned_at,payment_due_at)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;
-- name: GetPass :one
SELECT * FROM shuttle_passes WHERE registration_id=$1;
-- name: CreatePassTier :exec
INSERT INTO shuttle_pass_tiers(pass_id,tier_id,tier_number) VALUES($1,$2,$3);
-- name: GetPassTier :one
SELECT * FROM shuttle_pass_tiers WHERE pass_id=$1;
-- name: ListTierUsage :many
SELECT r.role,COALESCE(t.tier_id,'')::text AS tier_id,count(*) AS count
FROM shuttle_registrations r JOIN shuttle_passes p ON p.registration_id=r.id
LEFT JOIN shuttle_pass_tiers t ON t.pass_id=p.id
WHERE r.event_id=$1 AND r.state='assigned'
GROUP BY r.role,COALESCE(t.tier_id,'')
ORDER BY r.role,tier_id;
-- name: GetRegistrationOperation :one
SELECT * FROM shuttle_registration_operations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertRegistrationOperation :exec
INSERT INTO shuttle_registration_operations(bot_id,organization_id,actor,operation_id,request_hash,result) VALUES($1,$2,$3,$4,$5,$6);

-- name: ListEventRegistrations :many
SELECT r.* FROM shuttle_registrations r JOIN shuttle_users u ON u.id=r.user_id
WHERE r.event_id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3 AND r.id>$4 ORDER BY r.id LIMIT $5;
-- name: UserModuleAvailable :one
SELECT EXISTS(SELECT 1 FROM shuttle_events e
JOIN shuttle_registrations r ON r.event_id=e.id AND r.state='assigned'
JOIN shuttle_passes p ON p.registration_id=r.id
JOIN shuttle_users u ON u.id=r.user_id
WHERE e.id=$1 AND e.bot_id=$2 AND e.organization_id=$3 AND u.id=$4
AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id AND NOT u.revoked
AND $5::text=ANY(e.modules)) AS available;

-- name: GetAssignmentPreparation :one
SELECT p.* FROM shuttle_assignment_preparations p
WHERE p.bot_id=$1 AND p.organization_id=$2 AND p.actor=$3
AND (p.operation_id=$4 OR p.operation_id=(SELECT a.canonical_operation_id FROM shuttle_assignment_aliases a
 WHERE a.bot_id=$1 AND a.organization_id=$2 AND a.actor=$3 AND a.operation_id=$4));
-- name: InsertAssignmentPreparation :exec
INSERT INTO shuttle_assignment_preparations(bot_id,organization_id,actor,operation_id,proposal_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: GetUserEventContext :one
SELECT * FROM shuttle_user_event_context WHERE user_id=$1;
-- name: EnsureUserEventContext :exec
INSERT INTO shuttle_user_event_context(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING;
-- name: LockUserEventContext :one
SELECT * FROM shuttle_user_event_context WHERE user_id=$1 FOR UPDATE;
-- name: UpdateUserEventContext :one
UPDATE shuttle_user_event_context SET event_id=$2,version=version+1 WHERE user_id=$1 AND version=$3 RETURNING *;
-- name: GetRunEventContext :one
SELECT event_id FROM shuttle_run_event_context WHERE user_id=$1 AND run_id=$2;
-- name: SetRunEventContext :exec
INSERT INTO shuttle_run_event_context(user_id,run_id,event_id) VALUES($1,$2,$3)
ON CONFLICT(user_id,run_id) DO UPDATE SET event_id=EXCLUDED.event_id;
-- name: GetEventContextOperation :one
SELECT * FROM shuttle_event_context_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertEventContextOperation :exec
INSERT INTO shuttle_event_context_operations(user_id,operation_id,request_hash,result) VALUES($1,$2,$3,$4);
-- name: LockContextEvent :one
SELECT * FROM shuttle_events WHERE id=$1 AND bot_id=$2 AND organization_id=$3 FOR SHARE;
-- name: ListContextEvents :many
SELECT sqlc.embed(e),rules.document AS registration_rules,d.document AS deposit_policy FROM shuttle_events e
LEFT JOIN shuttle_event_registration_rules rules ON rules.event_id=e.id
LEFT JOIN shuttle_event_deposit_policy d ON d.event_id=e.id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND (sqlc.arg(event_id)::uuid='00000000-0000-0000-0000-000000000000' OR e.id=sqlc.arg(event_id))
AND e.id>sqlc.arg(after_id)::uuid
AND e.ends_at>sqlc.arg(at_time)::timestamptz AND sqlc.arg(module_id)::text=ANY(e.modules)
AND CASE WHEN sqlc.arg(module_id)::text='registrations'
 THEN rules.document IS NOT NULL AND (rules.document->>'closes_at')::timestamptz>sqlc.arg(at_time)::timestamptz
 ELSE EXISTS(SELECT 1 FROM shuttle_registrations r JOIN shuttle_passes p ON p.registration_id=r.id
 WHERE r.event_id=e.id AND r.user_id=sqlc.arg(user_id) AND r.state='assigned') END
ORDER BY CASE WHEN sqlc.arg(list_order)::boolean THEN 0 WHEN e.starts_at<=sqlc.arg(at_time)::timestamptz THEN 0 ELSE 1 END,
CASE WHEN sqlc.arg(list_order)::boolean THEN NULL ELSE e.starts_at END,e.id
LIMIT sqlc.arg(page_limit);

-- name: SetEventDepositPolicy :exec
INSERT INTO shuttle_event_deposit_policy(event_id,document) VALUES($1,$2)
ON CONFLICT(event_id) DO UPDATE SET document=EXCLUDED.document;
-- name: ReadEventDepositPolicy :one
SELECT document FROM shuttle_event_deposit_policy WHERE event_id=$1;
-- name: CreatePassDepositPolicy :exec
INSERT INTO shuttle_pass_deposit_policy(pass_id,document) VALUES($1,$2);
-- name: ReadPassDepositPolicy :one
SELECT document FROM shuttle_pass_deposit_policy WHERE pass_id=$1;

-- name: CreateFile :exec
INSERT INTO shuttle_files(id,user_id,task_id,telegram_id,unique_id,kind,mime_type,filename,size_bytes,message_id,sent_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);
-- name: GetOwnedFile :one
SELECT f.* FROM shuttle_files f JOIN shuttle_users u ON u.id=f.user_id
WHERE f.id=$1 AND u.id=$2 AND u.bot_id=$3 AND u.zitadel_org=$4 AND u.zitadel_subject=$5 AND NOT u.revoked;

-- name: ReadEventAuthority :one
SELECT a.* FROM shuttle_event_authorities a JOIN shuttle_events e ON e.id=a.event_id
JOIN shuttle_users u ON u.id=a.user_id
WHERE a.event_id=$1 AND a.user_id=$2 AND e.bot_id=$3 AND e.organization_id=$4
AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id AND NOT u.revoked;
-- name: ListEventAuthorities :many
SELECT a.* FROM shuttle_event_authorities a JOIN shuttle_users u ON u.id=a.user_id
WHERE a.event_id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3 AND NOT u.revoked
AND a.user_id>$4 ORDER BY a.user_id LIMIT $5;
-- name: SetEventAuthority :one
INSERT INTO shuttle_event_authorities(event_id,user_id,role,version) VALUES($1,$2,$3,1)
ON CONFLICT(event_id,user_id) DO UPDATE SET role=EXCLUDED.role,version=shuttle_event_authorities.version+1 RETURNING *;
-- name: GetAuthorityOperation :one
SELECT * FROM shuttle_authority_operations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertAuthorityOperation :exec
INSERT INTO shuttle_authority_operations(bot_id,organization_id,actor,operation_id,request_hash,result) VALUES($1,$2,$3,$4,$5,$6);
-- name: GetAuthorityPreparation :one
SELECT * FROM shuttle_authority_preparations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertAuthorityPreparation :exec
INSERT INTO shuttle_authority_preparations(bot_id,organization_id,actor,operation_id,proposal_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: LockAuthorityActor :one
SELECT * FROM shuttle_users WHERE bot_id=$1 AND zitadel_org=$2 AND zitadel_subject=$3 AND NOT revoked FOR SHARE;

-- name: ReadPaymentPass :one
SELECT r.* FROM shuttle_registrations r JOIN shuttle_passes p ON p.registration_id=r.id
WHERE p.id=$1 AND r.event_id=$2 AND r.state='assigned';
-- name: InsertPaymentEvidence :one
INSERT INTO shuttle_payment_evidence(id,event_id,pass_id,user_id,file_id,source_unique_id,amount_minor,currency,currency_scale,purpose,sent_at,due_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;
-- name: ReadPaymentEvidence :one
SELECT sqlc.embed(ev),coalesce(rv.decision,'pending')::text AS state,to_jsonb(rv) AS review FROM shuttle_payment_evidence ev
LEFT JOIN shuttle_payment_reviews rv ON rv.evidence_id=ev.id
WHERE ev.id=$1 AND ev.event_id=$2;
-- name: ListPaymentEvidence :many
SELECT sqlc.embed(ev),coalesce(rv.decision,'pending')::text AS state,to_jsonb(rv) AS review FROM shuttle_payment_evidence ev
LEFT JOIN shuttle_payment_reviews rv ON rv.evidence_id=ev.id
WHERE ev.event_id=$1 AND (sqlc.arg(owner_id)::uuid='00000000-0000-0000-0000-000000000000' OR ev.user_id=sqlc.arg(owner_id))
AND ev.id>sqlc.arg(after_id)::uuid ORDER BY ev.id LIMIT sqlc.arg(page_limit);
-- name: PaymentTotal :one
SELECT coalesce(sum(rv.amount_minor),0)::bigint AS amount FROM shuttle_payment_reviews rv
JOIN shuttle_payment_evidence ev ON ev.id=rv.evidence_id WHERE ev.pass_id=$1 AND rv.decision='confirmed';
-- name: ReadPaymentVersion :one
SELECT coalesce((SELECT version FROM shuttle_payment_versions WHERE pass_id=$1),0)::bigint AS version;
-- name: AdvancePaymentVersion :exec
INSERT INTO shuttle_payment_versions(pass_id,version) VALUES($1,1)
ON CONFLICT(pass_id) DO UPDATE SET version=shuttle_payment_versions.version+1;
-- name: InsertPaymentReview :one
INSERT INTO shuttle_payment_reviews(id,evidence_id,actor_id,actor_subject,authority_role,decision,amount_minor,reason)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;
-- name: GetPaymentOperation :one
SELECT * FROM shuttle_payment_operations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertPaymentOperation :exec
INSERT INTO shuttle_payment_operations(bot_id,organization_id,actor,operation_id,kind,request_hash,result) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: GetPaymentPreparation :one
SELECT * FROM shuttle_payment_preparations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertPaymentPreparation :exec
INSERT INTO shuttle_payment_preparations(bot_id,organization_id,actor,operation_id,proposal_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: HasUnrejectedPaymentSource :one
SELECT EXISTS(SELECT 1 FROM shuttle_payment_evidence ev
LEFT JOIN shuttle_payment_reviews rv ON rv.evidence_id=ev.id
WHERE ev.user_id=$1 AND ev.source_unique_id=$2 AND (rv.id IS NULL OR rv.decision='confirmed')
UNION ALL SELECT 1 FROM shuttle_order_evidence oe WHERE oe.user_id=$1 AND oe.source_unique_id=$2 AND oe.state<>'rejected') AS used;

-- name: GetPaymentEvidenceFile :one
SELECT f.* FROM shuttle_files f JOIN shuttle_payment_evidence ev ON ev.file_id=f.id
WHERE ev.id=$1 AND ev.event_id=$2 AND f.user_id=ev.user_id;
-- name: HasEventPaymentAuthority :one
SELECT EXISTS(SELECT 1 FROM shuttle_event_authorities a JOIN shuttle_events e ON e.id=a.event_id
WHERE a.user_id=$1 AND e.bot_id=$2 AND e.organization_id=$3 AND a.role IN ('ambassador','administrator')
AND 'registrations'=ANY(e.modules)) AS available;

-- name: PaymentEvidenceEvent :one
SELECT ev.event_id FROM shuttle_payment_evidence ev JOIN shuttle_events e ON e.id=ev.event_id
WHERE ev.id=$1 AND e.bot_id=$2 AND e.organization_id=$3;

-- name: ListPaymentEvents :many
SELECT sqlc.embed(e),coalesce(p.id,'00000000-0000-0000-0000-000000000000'::uuid)::uuid AS own_pass_id
FROM shuttle_events e
JOIN shuttle_users actor ON actor.id=sqlc.arg(user_id) AND actor.bot_id=e.bot_id
AND actor.zitadel_org=e.organization_id AND actor.zitadel_subject=sqlc.arg(subject) AND NOT actor.revoked
LEFT JOIN shuttle_registrations r ON r.event_id=e.id AND r.user_id=actor.id AND r.state='assigned'
LEFT JOIN shuttle_passes p ON p.registration_id=r.id
LEFT JOIN shuttle_event_authorities a ON a.event_id=e.id AND a.user_id=actor.id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'registrations'=ANY(e.modules) AND e.id>sqlc.arg(after_id)::uuid
AND (sqlc.arg(global_admin)::boolean OR p.id IS NOT NULL OR a.role IN ('ambassador','administrator'))
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: ListPaymentNotificationRecipients :many
SELECT u.id FROM shuttle_event_authorities a JOIN shuttle_users u ON u.id=a.user_id
WHERE a.event_id=$1 AND a.role IN ('ambassador','administrator') AND NOT u.revoked
AND u.bot_id=$2 AND u.zitadel_org=$3 AND u.zitadel_subject<>'' ORDER BY u.id;
-- name: InsertPaymentNotification :exec
INSERT INTO shuttle_payment_notifications(id,bot_id,organization_id,evidence_id,recipient_id,kind,document)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(evidence_id,recipient_id,kind) DO NOTHING;
-- name: GetPaymentNotification :one
SELECT * FROM shuttle_payment_notifications WHERE id=$1 AND bot_id=$2 AND organization_id=$3;
-- name: ReadyPaymentNotifications :many
SELECT id,version FROM shuttle_payment_notifications WHERE bot_id=$1 AND organization_id=$2
AND state IN ('pending','retryable') AND next_attempt_at<=clock_timestamp()
AND id>sqlc.arg(after_id)::uuid ORDER BY id LIMIT sqlc.arg(page_limit);
-- name: PaymentNotificationAllowed :one
SELECT EXISTS(SELECT 1 FROM shuttle_payment_notifications n
JOIN shuttle_payment_evidence ev ON ev.id=n.evidence_id
JOIN shuttle_events e ON e.id=ev.event_id
JOIN shuttle_users recipient ON recipient.id=n.recipient_id
JOIN shuttle_users owner ON owner.id=ev.user_id
WHERE n.id=$1 AND n.bot_id=$2 AND n.organization_id=$3
AND e.bot_id=n.bot_id AND e.organization_id=n.organization_id AND 'registrations'=ANY(e.modules)
AND recipient.bot_id=n.bot_id AND recipient.zitadel_org=n.organization_id
AND recipient.zitadel_subject<>'' AND NOT recipient.revoked
AND owner.bot_id=n.bot_id AND owner.zitadel_org=n.organization_id AND NOT owner.revoked
AND ((n.kind='submitted' AND NOT EXISTS(SELECT 1 FROM shuttle_payment_reviews rv WHERE rv.evidence_id=ev.id)
AND EXISTS(SELECT 1 FROM shuttle_event_authorities a WHERE a.event_id=e.id AND a.user_id=recipient.id
AND a.role IN ('ambassador','administrator')))
OR (n.kind IN ('confirmed','rejected') AND recipient.id=owner.id))) AS allowed;
-- name: ReservePaymentNotification :one
UPDATE shuttle_payment_notifications SET state='send_inflight',version=version+1,attempt_id=$3,payload=$4
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable') RETURNING *;
-- name: FinishPaymentNotification :one
UPDATE shuttle_payment_notifications SET state=$3,version=version+1,receipt=$4,
next_attempt_at=clock_timestamp()+sqlc.arg(delay_ms)::bigint*interval '1 millisecond'
WHERE id=$1 AND attempt_id=$2 AND state='send_inflight' RETURNING *;
-- name: BlockPaymentNotification :exec
UPDATE shuttle_payment_notifications SET state=$3,version=version+1
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable');
-- name: RecoverPaymentNotifications :execrows
UPDATE shuttle_payment_notifications SET state='delivery_unknown',version=version+1
WHERE bot_id=$1 AND organization_id=$2 AND state='send_inflight';
-- name: ListAdminPaymentNotifications :many
SELECT id,evidence_id,recipient_id,kind,state,version,next_attempt_at,created_at
FROM shuttle_payment_notifications WHERE bot_id=$1 AND organization_id=$2
AND (sqlc.arg(filter_state)::text='' OR state=sqlc.arg(filter_state)) AND id>sqlc.arg(after_id)::uuid
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: GetNotificationResolution :one
SELECT * FROM shuttle_notification_resolutions
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertNotificationResolution :exec
INSERT INTO shuttle_notification_resolutions
(bot_id,organization_id,actor,operation_id,proposal_id,notification_id,request_hash,expires_at,document,pair_notification_id,order_notification_id,massage_reminder_id,massage_specialist_notification_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13);
-- name: SaveNotificationResolution :execrows
UPDATE shuttle_notification_resolutions SET result=$5
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 AND result IS NULL;
-- name: ResolvePaymentNotification :one
UPDATE shuttle_payment_notifications SET state=$3,version=version+1,next_attempt_at=clock_timestamp()
WHERE id=$1 AND version=$2 AND state IN ('delivery_unknown','delivery_failed','delivery_blocked')
RETURNING *;

-- name: GetPairOperation :one
SELECT * FROM shuttle_pair_operations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertPairOperation :exec
INSERT INTO shuttle_pair_operations(bot_id,organization_id,actor,operation_id,kind,request_hash,result)
VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: GetPairInvitation :one
SELECT * FROM shuttle_pair_invitations WHERE id=$1 AND event_id=$2;
-- name: PendingOutgoingPairInvitation :one
SELECT * FROM shuttle_pair_invitations WHERE inviter_registration_id=$1 AND state='pending';
-- name: InsertPairInvitation :one
INSERT INTO shuttle_pair_invitations(id,event_id,inviter_registration_id,target_user_id,created_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: DecidePairInvitation :one
UPDATE shuttle_pair_invitations SET state=$3,version=version+1,decided_at=$4,pair_id=$5
WHERE id=$1 AND version=$2 AND state='pending' RETURNING *;
-- name: PairRegistrationHeld :one
SELECT EXISTS(SELECT 1 FROM shuttle_active_pair_members WHERE registration_id=$1)
OR EXISTS(SELECT 1 FROM shuttle_pair_invitations WHERE inviter_registration_id=$1 AND state='pending') AS held;
-- name: AdvancePairRegistration :one
UPDATE shuttle_registrations SET version=version+1 WHERE id=$1 AND event_id=$2 RETURNING *;
-- name: InsertPair :one
INSERT INTO shuttle_pairs(id,event_id,leader_registration_id,follower_registration_id,queued_at,created_at)
VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: InsertActivePairMembers :execrows
INSERT INTO shuttle_active_pair_members(pair_id,event_id,registration_id,role)
SELECT p.id,p.event_id,p.leader_registration_id,'leader' FROM shuttle_pairs p WHERE p.id=$1 AND p.event_id=$2
UNION ALL
SELECT p.id,p.event_id,p.follower_registration_id,'follower' FROM shuttle_pairs p WHERE p.id=$1 AND p.event_id=$2;
-- name: GetPair :one
SELECT * FROM shuttle_pairs WHERE id=$1 AND event_id=$2;
-- name: DissolvePair :one
UPDATE shuttle_pairs SET state='dissolved',version=version+1,dissolved_at=$3
WHERE id=$1 AND version=$2 AND state IN ('waitlisted','assigned') RETURNING *;
-- name: DeleteActivePairMembers :execrows
DELETE FROM shuttle_active_pair_members WHERE pair_id=$1;
-- name: HasActivePairMember :one
SELECT EXISTS(SELECT 1 FROM shuttle_active_pair_members WHERE registration_id=$1) AS active;

-- name: ExpiredPairInvitations :many
SELECT i.id,i.event_id FROM shuttle_pair_invitations i JOIN shuttle_events e ON e.id=i.event_id
WHERE e.bot_id=$1 AND e.organization_id=$2 AND i.state='pending' AND i.expires_at<=clock_timestamp()
AND i.id>sqlc.arg(after_id)::uuid ORDER BY i.id LIMIT sqlc.arg(page_limit);

-- name: GetPairPreparation :one
SELECT * FROM shuttle_pair_preparations WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4;
-- name: InsertPairPreparation :exec
INSERT INTO shuttle_pair_preparations(bot_id,organization_id,actor,operation_id,proposal_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: AssignPair :one
UPDATE shuttle_pairs SET state='assigned',version=version+1,assigned_at=$3
WHERE id=$1 AND version=$2 AND state='waitlisted' RETURNING *;
-- name: PairMemberMatches :one
SELECT EXISTS(SELECT 1 FROM shuttle_active_pair_members
WHERE pair_id=$1 AND event_id=$2 AND registration_id=$3 AND role=$4) AS matches;

-- name: HasPairAssignmentAuthority :one
SELECT EXISTS(SELECT 1 FROM shuttle_users u
WHERE u.id=sqlc.arg(user_id) AND u.bot_id=sqlc.arg(bot_id)
AND u.zitadel_org=sqlc.arg(organization_id) AND u.zitadel_subject=sqlc.arg(subject) AND NOT u.revoked
AND (sqlc.arg(global_admin)::boolean OR EXISTS(SELECT 1 FROM shuttle_event_authorities a
JOIN shuttle_events e ON e.id=a.event_id WHERE a.user_id=u.id AND a.role='administrator'
AND e.bot_id=u.bot_id AND e.organization_id=u.zitadel_org AND 'registrations'=ANY(e.modules)))) AS available;

-- name: ListPairAssignmentEvents :many
SELECT e.* FROM shuttle_events e JOIN shuttle_users u ON u.id=sqlc.arg(user_id)
AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id AND u.zitadel_subject=sqlc.arg(subject) AND NOT u.revoked
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'registrations'=ANY(e.modules) AND e.id>sqlc.arg(after_id)::uuid
AND (sqlc.arg(global_admin)::boolean OR EXISTS(SELECT 1 FROM shuttle_event_authorities a
WHERE a.event_id=e.id AND a.user_id=u.id AND a.role='administrator'))
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: ListPairAssignmentCandidates :many
SELECT sqlc.embed(p),l.user_id AS leader_user_id,l.version AS leader_version,
f.user_id AS follower_user_id,f.version AS follower_version
FROM shuttle_pairs p JOIN shuttle_registrations l ON l.id=p.leader_registration_id
JOIN shuttle_registrations f ON f.id=p.follower_registration_id
WHERE p.event_id=$1 AND p.state='waitlisted' AND p.id>sqlc.arg(after_id)::uuid
ORDER BY p.id LIMIT sqlc.arg(page_limit);

-- name: ListOwnedPairs :many
SELECT sqlc.embed(p),e.name AS event_name,e.timezone,e.starts_at,e.ends_at
FROM shuttle_pairs p JOIN shuttle_events e ON e.id=p.event_id
JOIN shuttle_registrations l ON l.id=p.leader_registration_id
JOIN shuttle_registrations f ON f.id=p.follower_registration_id
JOIN shuttle_users u ON u.id=sqlc.arg(user_id) AND u.bot_id=e.bot_id
AND u.zitadel_org=e.organization_id AND u.zitadel_subject=sqlc.arg(subject) AND NOT u.revoked
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND (l.user_id=u.id OR f.user_id=u.id) AND p.state IN ('waitlisted','assigned')
AND (sqlc.arg(event_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR e.id=sqlc.arg(event_id))
AND p.id>sqlc.arg(after_id)::uuid ORDER BY p.id LIMIT sqlc.arg(page_limit);

-- name: ListPairInvitations :many
SELECT sqlc.embed(i),e.name AS event_name,e.timezone,e.starts_at,e.ends_at,
r.user_id AS inviter_user_id,r.role AS inviter_role,
trim(coalesce(inviter.profile->>'first_name','')||' '||coalesce(inviter.profile->>'last_name',''))::text AS inviter_name
FROM shuttle_pair_invitations i JOIN shuttle_events e ON e.id=i.event_id
JOIN shuttle_registrations r ON r.id=i.inviter_registration_id
JOIN shuttle_users inviter ON inviter.id=r.user_id AND inviter.bot_id=e.bot_id AND inviter.zitadel_org=e.organization_id
JOIN shuttle_users actor ON actor.id=sqlc.arg(user_id) AND actor.bot_id=e.bot_id
AND actor.zitadel_org=e.organization_id AND actor.zitadel_subject=sqlc.arg(subject) AND NOT actor.revoked
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND ((sqlc.arg(direction)::text='incoming' AND i.target_user_id=actor.id)
OR (sqlc.arg(direction)::text='outgoing' AND r.user_id=actor.id))
AND (sqlc.arg(event_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR e.id=sqlc.arg(event_id))
AND (sqlc.arg(filter_state)::text='' OR i.state=sqlc.arg(filter_state))
AND i.id>sqlc.arg(after_id)::uuid ORDER BY i.id LIMIT sqlc.arg(page_limit);

-- name: InsertPairInvitationNotification :exec
INSERT INTO shuttle_pair_notifications(id,bot_id,organization_id,invitation_id,invitation_version,recipient_id,kind)
SELECT sqlc.arg(id),e.bot_id,e.organization_id,i.id,i.version,
CASE WHEN i.state IN ('pending','revoked') THEN i.target_user_id ELSE r.user_id END,i.state
FROM shuttle_pair_invitations i JOIN shuttle_events e ON e.id=i.event_id
JOIN shuttle_registrations r ON r.id=i.inviter_registration_id
WHERE i.id=sqlc.arg(invitation_id)
ON CONFLICT(invitation_id,invitation_version,recipient_id) DO NOTHING;

-- name: ReadyPairInvitationNotifications :many
SELECT n.* FROM shuttle_pair_notifications n
WHERE n.bot_id=$1 AND n.organization_id=$2 AND n.state IN ('pending','retryable')
AND n.next_attempt_at<=clock_timestamp() AND n.id>sqlc.arg(after_id)::uuid
ORDER BY n.id LIMIT sqlc.arg(page_limit);

-- name: GetPairInvitationNotification :one
SELECT sqlc.embed(n),sqlc.embed(i),e.name AS event_name,e.timezone,e.starts_at,e.ends_at,
trim(coalesce(u.profile->>'first_name','')||' '||coalesce(u.profile->>'last_name',''))::text AS inviter_name,
r.user_id AS inviter_user_id,r.role AS inviter_role
FROM shuttle_pair_notifications n JOIN shuttle_pair_invitations i ON i.id=n.invitation_id
JOIN shuttle_events e ON e.id=i.event_id
JOIN shuttle_registrations r ON r.id=i.inviter_registration_id JOIN shuttle_users u ON u.id=r.user_id
WHERE n.id=$1 AND n.bot_id=$2 AND n.organization_id=$3
AND e.bot_id=n.bot_id AND e.organization_id=n.organization_id;

-- name: PairInvitationNotificationAllowed :one
SELECT EXISTS(SELECT 1 FROM shuttle_pair_notifications n
JOIN shuttle_pair_invitations i ON i.id=n.invitation_id
JOIN shuttle_events e ON e.id=i.event_id
JOIN shuttle_registrations r ON r.id=i.inviter_registration_id
JOIN shuttle_users inviter ON inviter.id=r.user_id
JOIN shuttle_users target ON target.id=i.target_user_id
JOIN shuttle_users recipient ON recipient.id=n.recipient_id
WHERE n.id=$1 AND n.bot_id=$2 AND n.organization_id=$3
AND e.bot_id=n.bot_id AND e.organization_id=n.organization_id
AND recipient.bot_id=n.bot_id AND recipient.zitadel_org=n.organization_id
AND recipient.zitadel_subject<>'' AND NOT recipient.revoked
AND inviter.bot_id=n.bot_id AND inviter.zitadel_org=n.organization_id
AND target.bot_id=n.bot_id AND target.zitadel_org=n.organization_id
AND n.invitation_version=i.version AND n.kind=i.state
AND recipient.id=CASE WHEN n.kind IN ('pending','revoked') THEN target.id ELSE inviter.id END
AND (n.kind NOT IN ('pending','accepted') OR (NOT inviter.revoked AND NOT target.revoked))
AND (n.kind<>'pending' OR ('registrations'=ANY(e.modules) AND i.expires_at>clock_timestamp()))
AND (n.kind<>'accepted' OR EXISTS(SELECT 1 FROM shuttle_pairs p WHERE p.id=i.pair_id AND p.state IN ('waitlisted','assigned')))
) AS allowed;

-- name: ReservePairInvitationNotification :one
UPDATE shuttle_pair_notifications SET state='send_inflight',version=version+1,attempt_id=$3,payload=$4
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable') RETURNING *;
-- name: FinishPairInvitationNotification :execrows
UPDATE shuttle_pair_notifications SET state=$3,version=version+1,receipt=$4,
next_attempt_at=clock_timestamp()+sqlc.arg(delay_ms)::bigint*interval '1 millisecond'
WHERE id=$1 AND attempt_id=$2 AND state='send_inflight';
-- name: BlockPairInvitationNotification :exec
UPDATE shuttle_pair_notifications SET state='delivery_blocked',version=version+1
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable');
-- name: RecoverPairInvitationNotifications :exec
UPDATE shuttle_pair_notifications SET state='delivery_unknown',version=version+1
WHERE bot_id=$1 AND organization_id=$2 AND state='send_inflight';

-- name: ResolvePairInvitationNotification :one
UPDATE shuttle_pair_notifications SET state=$3,version=version+1,next_attempt_at=clock_timestamp()
WHERE id=$1 AND version=$2 AND state IN ('delivery_unknown','delivery_failed','delivery_blocked') RETURNING *;
-- name: ListAdminPairNotifications :many
SELECT * FROM shuttle_pair_notifications WHERE bot_id=$1 AND organization_id=$2
AND (sqlc.arg(filter_state)::text='' OR state=sqlc.arg(filter_state)) AND id>sqlc.arg(after_id)::uuid
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: GetLegacyArchivePreparation :one
SELECT *, expires_at>clock_timestamp() AS unexpired FROM shuttle_legacy_archive_preparations
WHERE user_id=$1 AND operation_id=$2;

-- name: InsertLegacyArchivePreparation :one
INSERT INTO shuttle_legacy_archive_preparations(id,user_id,operation_id,arguments,request_hash)
VALUES($1,$2,$3,$4,$5) RETURNING *;

-- name: GetLegacyArchiveOperation :one
SELECT * FROM shuttle_legacy_archive_operations WHERE user_id=$1 AND operation_id=$2;

-- name: InsertLegacyArchiveOperation :exec
INSERT INTO shuttle_legacy_archive_operations(user_id,operation_id,proposal_id,archive_id,request_hash,receipt)
VALUES($1,$2,$3,$4,$5,$6);

-- name: GetLegacyArchiveByDigest :one
SELECT * FROM shuttle_legacy_archives WHERE bot_id=$1 AND organization_id=$2 AND sha256=$3;

-- name: LegacyArchiveUsage :one
SELECT count(*)::bigint AS archives,coalesce(sum(size_bytes),0)::bigint AS bytes
FROM shuttle_legacy_archives WHERE bot_id=$1 AND organization_id=$2;

-- name: InsertLegacyArchive :one
INSERT INTO shuttle_legacy_archives(id,bot_id,organization_id,sha256,size_bytes,validation,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: InsertLegacyArchiveChunk :exec
INSERT INTO shuttle_legacy_archive_chunks(archive_id,ordinal,content) VALUES($1,$2,$3);

-- name: GetLegacyArchive :one
SELECT * FROM shuttle_legacy_archives WHERE id=$1 AND bot_id=$2 AND organization_id=$3;

-- name: GetLegacyArchiveChunk :one
SELECT content FROM shuttle_legacy_archive_chunks WHERE archive_id=$1 AND ordinal=$2;

-- name: LegacyArchiveChunkStats :one
SELECT count(*)::bigint AS chunks,coalesce(sum(octet_length(content)),0)::bigint AS bytes,
coalesce(min(ordinal),-1)::integer AS first_ordinal,coalesce(max(ordinal),-1)::integer AS last_ordinal
FROM shuttle_legacy_archive_chunks WHERE archive_id=$1;

-- name: GetHistoryPreparation :one
SELECT *,expires_at>clock_timestamp() AS unexpired FROM shuttle_legacy_history_preparations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertHistoryPreparation :one
INSERT INTO shuttle_legacy_history_preparations(id,user_id,operation_id,arguments,request_hash)
VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: GetHistoryOperation :one
SELECT * FROM shuttle_legacy_history_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertHistoryOperation :exec
INSERT INTO shuttle_legacy_history_operations(user_id,operation_id,proposal_id,archive_id,request_hash,receipt)
VALUES($1,$2,$3,$4,$5,$6);
-- name: GetHistoryApplication :one
SELECT receipt FROM shuttle_legacy_history_applications WHERE archive_id=$1 AND projection_version=$2;
-- name: InsertHistoryApplication :exec
INSERT INTO shuttle_legacy_history_applications(archive_id,projection_version,receipt) VALUES($1,$2,$3);
-- name: HistoryUsage :one
SELECT count(*)::bigint AS records,coalesce(sum(octet_length(projection::text)),0)::bigint AS bytes
FROM shuttle_legacy_history_records WHERE bot_id=$1 AND organization_id=$2;

-- name: OwnerHistoryPage :many
SELECT id,source_instance,collection_name,source_key,source_role,first_archive_id,first_line,projection_version,preview::jsonb
FROM shuttle_legacy_history_previews WHERE bot_id=$1 AND organization_id=$2 AND owner_telegram_id=$3 AND id>$4
ORDER BY id LIMIT $5;

-- name: OwnerHistoryText :one
SELECT substring(projection->>'content' FROM sqlc.arg(start_rune)::integer+1 FOR 4096)::text AS content,
length(projection->>'content')::integer AS total_runes
FROM shuttle_legacy_history_records WHERE id=$1 AND bot_id=$2 AND organization_id=$3 AND owner_telegram_id=$4
AND projection->>'kind'='message';

-- name: ListOwnAccountEvents :many
SELECT sqlc.embed(e),sqlc.embed(r)
FROM shuttle_registrations r JOIN shuttle_events e ON e.id=r.event_id
JOIN shuttle_users u ON u.id=r.user_id AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id
WHERE u.id=sqlc.arg(user_id) AND u.zitadel_subject=sqlc.arg(subject) AND NOT u.revoked
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND e.id>sqlc.arg(after_id)::uuid ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: ListOwnPaymentExpectations :many
SELECT sqlc.embed(e),sqlc.embed(r)
FROM shuttle_registrations r JOIN shuttle_passes p ON p.registration_id=r.id
JOIN shuttle_events e ON e.id=r.event_id
WHERE r.user_id=sqlc.arg(user_id) AND r.state='assigned'
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'registrations'=ANY(e.modules) AND p.id>sqlc.arg(after_id)::uuid
AND p.price_minor>(SELECT coalesce(sum(rv.amount_minor),0) FROM shuttle_payment_reviews rv
JOIN shuttle_payment_evidence ev ON ev.id=rv.evidence_id WHERE ev.pass_id=p.id AND rv.decision='confirmed')
ORDER BY p.id LIMIT sqlc.arg(page_limit);

-- name: PendingPassEvidence :one
SELECT count(*)::bigint FROM shuttle_payment_evidence ev
WHERE ev.pass_id=$1 AND NOT EXISTS(SELECT 1 FROM shuttle_payment_reviews rv WHERE rv.evidence_id=ev.id);

-- name: ReadOrdersCatalog :one
SELECT * FROM shuttle_orders_catalogs WHERE event_id=$1;
-- name: InsertOrdersCatalog :one
INSERT INTO shuttle_orders_catalogs(event_id,version,document) VALUES($1,1,$2)
ON CONFLICT(event_id) DO NOTHING RETURNING *;
-- name: UpdateOrdersCatalog :one
UPDATE shuttle_orders_catalogs SET document=$2,version=version+1
WHERE event_id=$1 AND version=$3 RETURNING *;
-- name: ReadOrdersCatalogPreparation :one
SELECT * FROM shuttle_orders_catalog_preparations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertOrdersCatalogPreparation :one
INSERT INTO shuttle_orders_catalog_preparations(id,user_id,operation_id,request_hash,arguments,expires_at)
VALUES($1,$2,$3,$4,$5,clock_timestamp()+interval '5 minutes') RETURNING *;
-- name: ReadOrdersCatalogOperation :one
SELECT * FROM shuttle_orders_catalog_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertOrdersCatalogOperation :exec
INSERT INTO shuttle_orders_catalog_operations(user_id,operation_id,request_hash,result) VALUES($1,$2,$3,$4);

-- name: ReadMassageSchedule :one
SELECT * FROM shuttle_massage_schedules WHERE event_id=$1;
-- name: InsertMassageSchedule :one
INSERT INTO shuttle_massage_schedules(event_id,version,document) VALUES($1,1,$2)
ON CONFLICT(event_id) DO NOTHING RETURNING *;
-- name: UpdateMassageSchedule :one
UPDATE shuttle_massage_schedules SET document=$2,version=version+1
WHERE event_id=$1 AND version=$3 RETURNING *;
-- name: ReadMassageSchedulePreparation :one
SELECT * FROM shuttle_massage_schedule_preparations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertMassageSchedulePreparation :one
INSERT INTO shuttle_massage_schedule_preparations(id,user_id,operation_id,request_hash,arguments,expires_at)
VALUES($1,$2,$3,$4,$5,clock_timestamp()+interval '5 minutes') RETURNING *;
-- name: ReadMassageScheduleOperation :one
SELECT * FROM shuttle_massage_schedule_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertMassageScheduleOperation :exec
INSERT INTO shuttle_massage_schedule_operations(user_id,operation_id,request_hash,result) VALUES($1,$2,$3,$4);

-- name: OwnerHistoryRegistrationSource :one
SELECT sqlc.embed(h),sqlc.embed(a)
FROM shuttle_legacy_history_records h
JOIN shuttle_legacy_history_memberships m ON m.record_id=h.id AND m.archive_id=h.first_archive_id
 AND m.source_role=h.source_role AND m.source_line=h.first_line
JOIN shuttle_legacy_archives a ON a.id=m.archive_id AND a.bot_id=h.bot_id AND a.organization_id=h.organization_id
JOIN shuttle_legacy_history_applications app ON app.archive_id=a.id AND app.projection_version=h.projection_version
WHERE h.id=$1 AND h.bot_id=$2 AND h.organization_id=$3 AND h.owner_telegram_id=$4 AND h.source_role='users';

-- name: InsertTelegramPrelink :exec
INSERT INTO shuttle_telegram_prelinks(id,bot_id,user_id,provider_issuer,assertion_jti,assertion_hash,organization,provider_id,external_subject,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(provider_issuer,assertion_jti) DO NOTHING;
-- name: LockTelegramPrelinkProof :one
SELECT * FROM shuttle_telegram_prelinks WHERE provider_issuer=$1 AND assertion_jti=$2 FOR UPDATE;
-- name: GetTelegramPrelink :one
SELECT * FROM shuttle_telegram_prelinks WHERE id=$1;
-- name: LockTelegramPrelink :one
SELECT * FROM shuttle_telegram_prelinks WHERE id=$1 FOR UPDATE;
-- name: CompleteTelegramPrelink :one
UPDATE shuttle_telegram_prelinks SET state='completed',subject=$2,completed_at=clock_timestamp()
WHERE id=$1 AND state='pending' AND expires_at>clock_timestamp() RETURNING *;

-- name: GetRegistrationDetails :one
SELECT * FROM shuttle_registration_details WHERE user_id=$1;

-- name: GetRegistrationDetailsRequest :one
SELECT * FROM shuttle_registration_details_requests WHERE id=$1 AND user_id=$2;
-- name: LatestRegistrationDetailsRequest :one
SELECT * FROM shuttle_registration_details_requests WHERE user_id=$1 AND event_id=$2 AND superseded_at IS NULL
ORDER BY created_at DESC,id DESC LIMIT 1;

-- name: ListRegistrationDetailsRequests :many
SELECT * FROM shuttle_registration_details_requests WHERE user_id=$1 AND id>$2 AND superseded_at IS NULL
ORDER BY id LIMIT $3;
-- name: RecentRegistrationDetailsRequests :many
SELECT r.* FROM shuttle_registration_details_requests r JOIN shuttle_tasks t ON t.user_id=r.user_id
WHERE t.id=$1 AND r.user_id=$2 AND r.created_at<t.created_at AND r.superseded_at IS NULL
ORDER BY r.created_at DESC,r.id DESC LIMIT 5;

-- name: ReadOrdersSelection :one
SELECT result FROM shuttle_orders_selections WHERE user_id=$1 AND event_id=$2;
-- name: ReadOrdersSelectionOperation :one
SELECT event_id,request_hash,result FROM shuttle_orders_selection_operations WHERE user_id=$1 AND operation_id=$2;
-- name: SaveOrdersSelection :exec
INSERT INTO shuttle_orders_selections(user_id,event_id,version,result) VALUES($1,$2,$3,$4)
ON CONFLICT(user_id,event_id) DO UPDATE SET version=EXCLUDED.version,result=EXCLUDED.result;
-- name: InsertOrdersSelectionOperation :exec
INSERT INTO shuttle_orders_selection_operations(user_id,operation_id,event_id,request_hash,result) VALUES($1,$2,$3,$4,$5);

-- name: ListOwnOrdersSelectionEvents :many
SELECT e.* FROM shuttle_events e
JOIN shuttle_registrations r ON r.event_id=e.id AND r.user_id=$1 AND r.state='assigned'
JOIN shuttle_passes p ON p.registration_id=r.id
WHERE e.bot_id=$2 AND e.organization_id=$3 AND 'orders'=ANY(e.modules) AND e.id>$4
ORDER BY e.id LIMIT $5;

-- name: ReadPaymentReviewWorkspaceGate :one
SELECT w.state, NOT EXISTS(SELECT 1 FROM shuttle_payment_review_workspaces newer
 WHERE newer.actor_id=w.actor_id AND newer.evidence_id=w.evidence_id AND newer.revision>w.revision) AS is_current
FROM shuttle_payment_review_workspaces w
WHERE w.id::text=sqlc.arg(operation_id)::text AND w.actor_id=sqlc.arg(user_id);

-- name: CompletePaymentReviewWorkspace :exec
UPDATE shuttle_payment_review_workspaces SET state='completed'
WHERE id::text=sqlc.arg(operation_id)::text AND actor_id=sqlc.arg(user_id);

-- name: ListPaymentReviewEvents :many
SELECT e.* FROM shuttle_events e
JOIN shuttle_users actor ON actor.id=sqlc.arg(user_id) AND actor.bot_id=e.bot_id
AND actor.zitadel_org=e.organization_id AND actor.zitadel_subject=sqlc.arg(subject) AND NOT actor.revoked
LEFT JOIN shuttle_event_authorities a ON a.event_id=e.id AND a.user_id=actor.id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'registrations'=ANY(e.modules) AND e.id>sqlc.arg(after_id)::uuid
AND (sqlc.arg(global_admin)::boolean OR a.role IN ('ambassador','administrator'))
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: ReadPaymentReviewPayer :one
SELECT u.id AS user_id,
 left(coalesce(nullif(btrim(p.full_name),''),
 nullif(btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name')),''),''),256)::text AS full_name
FROM shuttle_payment_evidence e
JOIN shuttle_users u ON u.id=e.user_id
LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
WHERE e.id=$1 AND e.event_id=$2 AND u.bot_id=$3 AND u.zitadel_org=$4;

-- name: ReadPaymentReviewPresentation :one
SELECT event.name AS event_name,
 left(coalesce(nullif(btrim(owner_profile.full_name),''),
 nullif(btrim(concat_ws(' ',owner.profile->>'first_name',owner.profile->>'last_name')),''),owner.id::text),256)::text AS payer_name,
 coalesce(actor_profile.bot_language,'auto')::text AS language_preference,
 coalesce(actor.profile->>'language_code','')::text AS telegram_language,
 coalesce(settings.document->>'default_language','en')::text AS default_language
FROM shuttle_payment_evidence evidence
JOIN shuttle_events event ON event.id=evidence.event_id
JOIN shuttle_users owner ON owner.id=evidence.user_id
JOIN shuttle_users actor ON actor.id=sqlc.arg(actor_id)
LEFT JOIN shuttle_user_profiles owner_profile ON owner_profile.user_id=owner.id
LEFT JOIN shuttle_user_profiles actor_profile ON actor_profile.user_id=actor.id
JOIN shuttle_settings settings ON settings.id=true
WHERE evidence.id=sqlc.arg(evidence_id) AND event.id=sqlc.arg(event_id)
AND event.bot_id=sqlc.arg(bot_id) AND event.organization_id=sqlc.arg(organization)
AND actor.bot_id=event.bot_id AND actor.zitadel_org=event.organization_id
AND actor.zitadel_subject=sqlc.arg(subject) AND NOT actor.revoked
AND owner.bot_id=event.bot_id AND owner.zitadel_org=event.organization_id;


-- name: InsertOrderPreparation :exec
INSERT INTO shuttle_order_preparations(id,user_id,event_id,source_draft_version,request_hash,arguments,snapshot,created_at,expires_at,order_kind)
VALUES(sqlc.arg(id),sqlc.arg(user_id),sqlc.arg(event_id),sqlc.arg(source_draft_version),sqlc.arg(request_hash),sqlc.arg(arguments),sqlc.arg(snapshot),sqlc.arg(created_at),sqlc.arg(expires_at),'service');
-- name: ReadOrderPreparation :one
SELECT * FROM shuttle_order_preparations WHERE id=sqlc.arg(id) AND user_id=sqlc.arg(user_id) AND event_id=sqlc.arg(event_id) AND order_kind='service';
-- name: FindPendingOrderPreparation :one
SELECT * FROM shuttle_order_preparations WHERE user_id=sqlc.arg(user_id) AND event_id=sqlc.arg(event_id)
 AND order_kind='service' AND source_draft_version=sqlc.arg(source_draft_version) AND request_hash=sqlc.arg(request_hash)
 AND consumed_at IS NULL AND expires_at>sqlc.arg(observed_at) ORDER BY created_at DESC,id DESC LIMIT 1;
-- name: ConsumeOrderPreparation :execrows
UPDATE shuttle_order_preparations SET consumed_at=sqlc.arg(observed_at)
WHERE id=sqlc.arg(id) AND user_id=sqlc.arg(user_id) AND event_id=sqlc.arg(event_id) AND consumed_at IS NULL AND order_kind='service';
-- name: ReadOrderOperations :many
SELECT * FROM shuttle_order_operations WHERE user_id=sqlc.arg(user_id) AND operation_id=sqlc.arg(operation_id) ORDER BY kind;
-- name: InsertOrderOperation :exec
INSERT INTO shuttle_order_operations(user_id,operation_id,kind,event_id,preparation_id,order_id,business_hash,request_hash,result,source,order_kind)
VALUES(sqlc.arg(user_id),sqlc.arg(operation_id),sqlc.arg(kind),sqlc.arg(event_id),sqlc.narg(preparation_id),sqlc.narg(order_id),sqlc.arg(business_hash),sqlc.arg(request_hash),sqlc.arg(result),sqlc.arg(source),'service');
-- name: ReadSubmittedOrder :one
SELECT * FROM shuttle_orders WHERE id=sqlc.arg(id) AND user_id=sqlc.arg(user_id) AND event_id=sqlc.arg(event_id) AND order_kind='service';
-- name: ReadCanonicalOrder :one
SELECT * FROM shuttle_orders WHERE user_id=sqlc.arg(user_id) AND event_id=sqlc.arg(event_id) AND source_draft_version=sqlc.arg(source_draft_version) AND order_kind='service';
-- name: InsertSubmittedOrder :exec
INSERT INTO shuttle_orders(id,user_id,event_id,source_draft_version,preparation_id,request_hash,version,state,submitted_result,sales_closes_at,submitted_at,order_kind)
VALUES(sqlc.arg(id),sqlc.arg(user_id),sqlc.arg(event_id),sqlc.arg(source_draft_version),sqlc.arg(preparation_id),sqlc.arg(request_hash),1,'submitted',sqlc.arg(submitted_result),sqlc.arg(sales_closes_at),sqlc.arg(submitted_at),'service');
-- name: CancelSubmittedOrder :one
UPDATE shuttle_orders AS o SET version=o.version+1,state='cancelled',cancelled_at=sqlc.arg(observed_at)
WHERE o.id=sqlc.arg(id) AND o.user_id=sqlc.arg(user_id) AND o.event_id=sqlc.arg(event_id)
 AND o.order_kind='service' AND o.version=sqlc.arg(expected_version) AND o.version<9223372036854775807 AND o.state='submitted' AND o.sales_closes_at>sqlc.arg(observed_at)
 AND NOT EXISTS(SELECT 1 FROM shuttle_order_evidence ev WHERE ev.order_id=o.id)
RETURNING o.*;
-- name: ListOwnSubmittedOrders :many
SELECT * FROM shuttle_orders WHERE user_id=sqlc.arg(user_id) AND event_id=sqlc.arg(event_id) AND id>sqlc.arg(after_id)
AND order_kind='service' ORDER BY id LIMIT sqlc.arg(page_limit);


-- name: OrderResourceUsage :many
SELECT resource_id,sum(units)::bigint AS units FROM shuttle_order_allocations
WHERE event_id=$1 AND state IN ('held','occupied') GROUP BY resource_id ORDER BY resource_id;
-- name: ReadOrderEvidence :one
SELECT * FROM shuttle_order_evidence WHERE id=$1 AND event_id=$2;
-- name: HasOrderEvidence :one
SELECT EXISTS(SELECT 1 FROM shuttle_order_evidence WHERE order_id=$1) AS present;
-- name: HasActiveOrderEvidence :one
SELECT EXISTS(SELECT 1 FROM shuttle_order_evidence WHERE order_id=$1 AND state<>'rejected') AS present;
-- name: InsertOrderEvidence :one
INSERT INTO shuttle_order_evidence(id,order_id,event_id,user_id,file_id,source_unique_id,reported_amount_minor,quoted_amount_minor,currency,currency_scale,sent_at,submitted_at,due_at,admission_state,reason,state)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$14) RETURNING *;
-- name: InsertOrderAllocation :exec
INSERT INTO shuttle_order_allocations(evidence_id,event_id,resource_id,units,state) VALUES($1,$2,$3,$4,'held');
-- name: ChangeOrderAllocations :exec
UPDATE shuttle_order_allocations SET state=$2 WHERE evidence_id=$1 AND state='held';
-- name: ReviewOrderEvidence :one
UPDATE shuttle_order_evidence SET state=$2,version=2 WHERE id=$1 AND version=1 RETURNING *;
-- name: ReadOrderEvidenceOperation :one
SELECT * FROM shuttle_order_evidence_operations WHERE user_id=$1 AND operation_id=$2 AND kind=$3;
-- name: InsertOrderEvidenceOperation :exec
INSERT INTO shuttle_order_evidence_operations(user_id,operation_id,kind,event_id,request_hash,result) VALUES($1,$2,$3,$4,$5,$6);
-- name: ReadOrderEvidencePreparation :one
SELECT * FROM shuttle_order_evidence_preparations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertOrderEvidencePreparation :exec
INSERT INTO shuttle_order_evidence_preparations(id,user_id,operation_id,event_id,request_hash,document,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7);
-- name: InsertOrderEvidenceReview :exec
INSERT INTO shuttle_order_evidence_reviews(evidence_id,actor_id,actor_subject,authority_role,decision,amount_minor,reason,reviewed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: InsertOrderEvidenceNotice :exec
INSERT INTO shuttle_order_evidence_notices(id,evidence_id,recipient_id,kind,document) VALUES($1,$2,$3,$4,$5);

-- name: LockOrderEvidenceOwner :one
SELECT id FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3 FOR SHARE;
-- name: ReadOrderEvidenceReview :one
SELECT * FROM shuttle_order_evidence_reviews WHERE evidence_id=$1;


-- name: OrderEvidenceEvent :one
SELECT ev.event_id FROM shuttle_order_evidence ev JOIN shuttle_events e ON e.id=ev.event_id
WHERE ev.id=$1 AND e.bot_id=$2 AND e.organization_id=$3;
-- name: GetOrderEvidenceFile :one
SELECT f.* FROM shuttle_files f
JOIN shuttle_order_evidence ev ON ev.file_id=f.id AND ev.user_id=f.user_id
JOIN shuttle_users owner ON owner.id=ev.user_id
WHERE ev.id=$1 AND ev.event_id=$2 AND owner.bot_id=$3 AND owner.zitadel_org=$4;

-- name: HasOrdersEvidenceAuthority :one
SELECT EXISTS(SELECT 1 FROM shuttle_event_authorities a JOIN shuttle_events e ON e.id=a.event_id
WHERE a.user_id=$1 AND e.bot_id=$2 AND e.organization_id=$3 AND a.role IN ('ambassador','administrator')
AND 'orders'=ANY(e.modules)) AS available;
-- name: ListOrderEvidence :many
SELECT ev.* FROM shuttle_order_evidence ev JOIN shuttle_users owner ON owner.id=ev.user_id
WHERE ev.event_id=sqlc.arg(event_id) AND owner.bot_id=sqlc.arg(bot_id) AND owner.zitadel_org=sqlc.arg(organization_id)
 AND (sqlc.arg(owner_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR ev.user_id=sqlc.arg(owner_id))
 AND (sqlc.arg(order_id)::uuid='00000000-0000-0000-0000-000000000000'::uuid OR ev.order_id=sqlc.arg(order_id))
 AND (NOT sqlc.arg(unresolved_only)::boolean OR ev.state IN ('pending','exhausted','needs_decision'))
 AND ev.id>sqlc.arg(after_id) ORDER BY ev.id LIMIT sqlc.arg(page_limit);

-- name: ReadOrderReviewWorkspace :one
SELECT w.*,p.document,NOT EXISTS(SELECT 1 FROM shuttle_order_review_workspaces later
 WHERE later.actor_id=w.actor_id AND later.evidence_id=w.evidence_id AND later.revision>w.revision) AS current
FROM shuttle_order_review_workspaces w JOIN shuttle_order_evidence_preparations p ON p.id=w.proposal_id
WHERE w.id=$1 AND w.actor_id=$2;
-- name: CurrentOrderReviewWorkspace :one
SELECT w.id FROM shuttle_order_review_workspaces w WHERE w.actor_id=$1 AND w.event_id=$2 AND w.evidence_id=$3
ORDER BY w.revision DESC LIMIT 1;
-- name: LatestOrderReviewRevision :one
SELECT coalesce(max(revision),0)::bigint FROM shuttle_order_review_workspaces WHERE actor_id=$1 AND evidence_id=$2;
-- name: InsertOrderReviewWorkspace :exec
INSERT INTO shuttle_order_review_workspaces(id,actor_id,event_id,evidence_id,revision,proposal_id,state)
VALUES($1,$2,$3,$4,$5,$6,'prepared');
-- name: CancelOrderReviewWorkspace :execrows
UPDATE shuttle_order_review_workspaces SET state='cancelled' WHERE id=$1 AND actor_id=$2 AND state='prepared';
-- name: CompleteOrderReviewWorkspace :execrows
UPDATE shuttle_order_review_workspaces SET state='completed' WHERE id=$1 AND actor_id=$2 AND state='prepared';
-- name: ReadOrderReviewCall :one
SELECT * FROM shuttle_order_review_calls WHERE actor_id=$1 AND operation_id=$2;
-- name: InsertOrderReviewCall :exec
INSERT INTO shuttle_order_review_calls(actor_id,operation_id,kind,source,request_hash,workspace_id,proposal_id,expires_at,document,result,completed_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);
-- name: CompleteOrderReviewCall :execrows
UPDATE shuttle_order_review_calls SET result=$3,completed_at=clock_timestamp()
WHERE actor_id=$1 AND operation_id=$2 AND kind='commit' AND result IS NULL;

-- name: HasOrderReviewWorkspaceHistory :one
SELECT EXISTS(SELECT 1 FROM shuttle_order_review_workspaces WHERE actor_id=$1 AND event_id=$2 AND evidence_id=$3) AS present;

-- name: ReadOrderReviewPresentation :one
SELECT left(coalesce(nullif(btrim(owner_profile.full_name),''),
 nullif(btrim(concat_ws(' ',owner.profile->>'first_name',owner.profile->>'last_name')),''),owner.id::text),256)::text AS payer_name,
 coalesce(actor_profile.bot_language,'auto')::text AS language_preference,
 coalesce(actor.profile->>'language_code','')::text AS telegram_language,
 coalesce(settings.document->>'default_language','en')::text AS default_language
FROM shuttle_order_evidence evidence JOIN shuttle_events event ON event.id=evidence.event_id
JOIN shuttle_users owner ON owner.id=evidence.user_id JOIN shuttle_users actor ON actor.id=sqlc.arg(actor_id)
LEFT JOIN shuttle_user_profiles owner_profile ON owner_profile.user_id=owner.id
LEFT JOIN shuttle_user_profiles actor_profile ON actor_profile.user_id=actor.id
JOIN shuttle_settings settings ON settings.id=true
WHERE evidence.id=sqlc.arg(evidence_id) AND event.id=sqlc.arg(event_id)
AND event.bot_id=sqlc.arg(bot_id) AND event.organization_id=sqlc.arg(organization)
AND actor.bot_id=event.bot_id AND actor.zitadel_org=event.organization_id
AND actor.zitadel_subject=sqlc.arg(subject) AND NOT actor.revoked
AND owner.bot_id=event.bot_id AND owner.zitadel_org=event.organization_id;


-- name: ListMiniAppOrderEvidenceFiles :many
SELECT id,left(filename,256)::text AS filename,left(mime_type,128)::text AS mime_type,
 left(kind,32)::text AS kind,size_bytes,sent_at,accepted_at
FROM shuttle_files WHERE user_id=sqlc.arg(user_id) AND id>sqlc.arg(after_id)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: ListOrderReviewEvents :many
SELECT e.* FROM shuttle_events e
JOIN shuttle_users actor ON actor.id=sqlc.arg(user_id) AND actor.bot_id=e.bot_id
AND actor.zitadel_org=e.organization_id AND actor.zitadel_subject=sqlc.arg(subject) AND NOT actor.revoked
LEFT JOIN shuttle_event_authorities a ON a.event_id=e.id AND a.user_id=actor.id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'orders'=ANY(e.modules) AND e.id>sqlc.arg(after_id)::uuid
AND (sqlc.arg(global_admin)::boolean OR a.role IN ('ambassador','administrator'))
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: GetOrderNotification :one
SELECT sqlc.embed(n),o.submitted_result FROM shuttle_order_evidence_notices n
JOIN shuttle_order_evidence ev ON ev.id=n.evidence_id
JOIN shuttle_orders o ON o.id=ev.order_id
JOIN shuttle_events e ON e.id=ev.event_id
WHERE n.id=$1 AND e.bot_id=$2 AND e.organization_id=$3;
-- name: ReadyOrderNotifications :many
SELECT n.id,n.version FROM shuttle_order_evidence_notices n
JOIN shuttle_order_evidence ev ON ev.id=n.evidence_id
JOIN shuttle_events e ON e.id=ev.event_id
WHERE e.bot_id=$1 AND e.organization_id=$2
AND n.state IN ('pending','retryable') AND n.next_attempt_at<=clock_timestamp()
AND n.id>sqlc.arg(after_id)::uuid ORDER BY n.id LIMIT sqlc.arg(page_limit);
-- name: OrderNotificationRecipient :one
SELECT sqlc.embed(u),EXISTS(SELECT 1 FROM shuttle_event_authorities a
WHERE a.event_id=e.id AND a.user_id=u.id AND a.role IN ('ambassador','administrator')) AS reviewer
FROM shuttle_order_evidence_notices n
JOIN shuttle_order_evidence ev ON ev.id=n.evidence_id
JOIN shuttle_events e ON e.id=ev.event_id
JOIN shuttle_users u ON u.id=n.recipient_id
WHERE n.id=$1 AND e.bot_id=$2 AND e.organization_id=$3 AND 'orders'=ANY(e.modules)
AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id AND NOT u.revoked AND u.zitadel_subject<>''
AND n.kind=ev.state
AND (u.id=ev.user_id OR EXISTS(SELECT 1 FROM shuttle_event_authorities a
 WHERE a.event_id=e.id AND a.user_id=u.id AND a.role IN ('ambassador','administrator')));
-- name: ReserveOrderNotification :one
UPDATE shuttle_order_evidence_notices SET state='send_inflight',version=version+1,attempt_id=$3,payload=$4
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable') RETURNING *;
-- name: FinishOrderNotification :execrows
UPDATE shuttle_order_evidence_notices SET state=$3,version=version+1,receipt=$4,
next_attempt_at=clock_timestamp()+sqlc.arg(delay_ms)::bigint*interval '1 millisecond'
WHERE id=$1 AND attempt_id=$2 AND state='send_inflight';
-- name: BlockOrderNotification :execrows
UPDATE shuttle_order_evidence_notices SET state='delivery_blocked',version=version+1
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable');
-- name: RecoverOrderNotifications :execrows
UPDATE shuttle_order_evidence_notices n SET state='delivery_unknown',version=n.version+1
FROM shuttle_order_evidence ev,shuttle_events e
WHERE ev.id=n.evidence_id AND e.id=ev.event_id AND e.bot_id=$1 AND e.organization_id=$2 AND n.state='send_inflight';

-- name: ResolveOrderNotification :one
UPDATE shuttle_order_evidence_notices SET state=$3,version=version+1,next_attempt_at=clock_timestamp()
WHERE id=$1 AND version=$2 AND state IN ('delivery_unknown','delivery_failed','delivery_blocked')
RETURNING *;
-- name: ListAdminOrderNotifications :many
SELECT n.id,n.evidence_id,n.recipient_id,n.kind,n.state,n.version,n.next_attempt_at,n.created_at
FROM shuttle_order_evidence_notices n
JOIN shuttle_order_evidence ev ON ev.id=n.evidence_id JOIN shuttle_events e ON e.id=ev.event_id
WHERE e.bot_id=$1 AND e.organization_id=$2
AND (sqlc.arg(filter_state)::text='' OR n.state=sqlc.arg(filter_state)) AND n.id>sqlc.arg(after_id)::uuid
ORDER BY n.id LIMIT sqlc.arg(page_limit);

-- name: ReadMassagePreparationByOperation :one
SELECT * FROM shuttle_massage_preparations WHERE user_id=$1 AND operation_id=$2;
-- name: ReadMassagePreparation :one
SELECT * FROM shuttle_massage_preparations WHERE id=$1 AND event_id=$2 AND user_id=$3;
-- name: InsertMassagePreparation :one
INSERT INTO shuttle_massage_preparations(id,event_id,user_id,operation_id,kind,request_hash,arguments,snapshot,booking_id,source,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,clock_timestamp()+interval '5 minutes') RETURNING *;
-- name: ConsumeMassagePreparation :execrows
UPDATE shuttle_massage_preparations SET booking_id=$2,consumed_at=clock_timestamp(),consumed_result=$3
WHERE id=$1 AND consumed_at IS NULL;
-- name: ReadMassageBooking :one
SELECT * FROM shuttle_massage_bookings WHERE id=$1 AND event_id=$2 AND user_id=$3;
-- name: ListOwnMassageBookings :many
SELECT * FROM shuttle_massage_bookings WHERE event_id=$1 AND user_id=$2 AND id>$3 ORDER BY id LIMIT $4;
-- name: ReadMassageBookingOccupations :many
SELECT user_id,specialist_id,starts_at,occupancy_ends_at,requires_table FROM shuttle_massage_bookings WHERE event_id=$1 AND state='booked'
AND starts_at<sqlc.arg(until_at)::timestamptz AND occupancy_ends_at>sqlc.arg(from_at)::timestamptz ORDER BY starts_at,id;
-- name: CountOwnPartyMassages :one
SELECT count(*) FROM shuttle_massage_bookings WHERE event_id=$1 AND user_id=$2 AND window_id=$3 AND state='booked';
-- name: InsertMassageBooking :one
INSERT INTO shuttle_massage_bookings(id,event_id,user_id,preparation_id,window_id,specialist_id,service_id,
starts_at,treatment_ends_at,occupancy_ends_at,requires_table,currency,currency_scale,price_minor,snapshot,source)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING *;
-- name: CancelMassageBooking :one
UPDATE shuttle_massage_bookings SET state='cancelled',version=version+1,cancelled_at=clock_timestamp()
WHERE id=$1 AND event_id=$2 AND user_id=$3 AND version=$4 AND state='booked' RETURNING *;
-- name: ReadMassageOperation :one
SELECT * FROM shuttle_massage_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertMassageOperation :exec
INSERT INTO shuttle_massage_operations(user_id,operation_id,event_id,kind,request_hash,preparation_id,booking_id,result,source)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: ReadFutureMassageBookings :many
SELECT * FROM shuttle_massage_bookings WHERE event_id=$1 AND state='booked'
AND occupancy_ends_at>clock_timestamp() AND id>sqlc.arg(after_id)::uuid ORDER BY id LIMIT 100;
-- name: MassageBookingsOutsideEvent :one
SELECT EXISTS(SELECT 1 FROM shuttle_massage_bookings WHERE event_id=$1 AND state='booked'
AND occupancy_ends_at>clock_timestamp()
AND (starts_at<sqlc.arg(starts_at)::timestamptz OR occupancy_ends_at>sqlc.arg(ends_at)::timestamptz)) AS conflicts;
-- name: ReadSettingsForMassage :one
SELECT * FROM shuttle_settings WHERE id=true FOR SHARE;

-- name: MassageWindowPeakTableUse :one
WITH boundaries AS (
 SELECT greatest(b.starts_at,sqlc.arg(from_at)::timestamptz) AS at,1 AS delta
 FROM shuttle_massage_bookings b WHERE b.event_id=sqlc.arg(event_id)::uuid AND b.state='booked' AND b.requires_table AND b.occupancy_ends_at>clock_timestamp()
 AND b.starts_at<sqlc.arg(until_at)::timestamptz AND b.occupancy_ends_at>sqlc.arg(from_at)::timestamptz
 UNION ALL
 SELECT least(b.occupancy_ends_at,sqlc.arg(until_at)::timestamptz) AS at,-1 AS delta
 FROM shuttle_massage_bookings b WHERE b.event_id=sqlc.arg(event_id)::uuid AND b.state='booked' AND b.requires_table AND b.occupancy_ends_at>clock_timestamp()
 AND b.starts_at<sqlc.arg(until_at)::timestamptz AND b.occupancy_ends_at>sqlc.arg(from_at)::timestamptz
), changes AS (SELECT at,sum(delta) AS delta FROM boundaries GROUP BY at),
loads AS (SELECT sum(delta) OVER(ORDER BY at) AS used FROM changes)
SELECT coalesce(max(used),0)::bigint AS peak FROM loads;

-- name: ListOwnMassageEvents :many
SELECT e.* FROM shuttle_events e
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND e.id>sqlc.arg(after_id)::uuid
AND (EXISTS(SELECT 1 FROM shuttle_massage_bookings b WHERE b.event_id=e.id AND b.user_id=sqlc.arg(user_id))
 OR EXISTS(SELECT 1 FROM shuttle_massage_preparations pending WHERE pending.event_id=e.id
 AND pending.user_id=sqlc.arg(user_id) AND pending.consumed_at IS NULL AND pending.expires_at>clock_timestamp())
 OR ('massage'=ANY(e.modules) AND EXISTS(SELECT 1 FROM shuttle_registrations r
 JOIN shuttle_passes p ON p.registration_id=r.id WHERE r.event_id=e.id AND r.user_id=sqlc.arg(user_id) AND r.state='assigned')))
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: ListOwnPendingMassagePreparations :many
SELECT * FROM shuttle_massage_preparations
WHERE event_id=$1 AND user_id=$2 AND id>$3 AND consumed_at IS NULL AND expires_at>clock_timestamp()
ORDER BY id LIMIT $4;

-- name: ListTaskPendingMassagePreparations :many
SELECT p.* FROM shuttle_tasks task
JOIN shuttle_massage_preparations p ON p.user_id=task.user_id
JOIN shuttle_events e ON e.id=p.event_id
WHERE task.id=sqlc.arg(task_id)::uuid AND task.user_id=sqlc.arg(user_id)::uuid
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND p.created_at<task.created_at AND p.consumed_at IS NULL AND p.expires_at>clock_timestamp()
ORDER BY p.created_at DESC,p.id LIMIT 5;

-- name: ListTaskMassageOperations :many
SELECT op.operation_id,op.event_id,op.kind,op.source,op.preparation_id,op.booking_id,
coalesce(op.result->>'state','')::text AS original_state,op.created_at
FROM shuttle_tasks task JOIN shuttle_massage_operations op ON op.user_id=task.user_id
JOIN shuttle_events e ON e.id=op.event_id
WHERE task.id=sqlc.arg(task_id)::uuid AND task.user_id=sqlc.arg(user_id)::uuid
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND op.created_at<task.created_at ORDER BY op.created_at DESC,op.operation_id LIMIT 5;

-- name: ListEventMassageBookings :many
SELECT * FROM shuttle_massage_bookings WHERE event_id=$1 AND id>$2 ORDER BY id LIMIT $3;

-- name: InsertMassageReminder :exec
INSERT INTO shuttle_massage_reminders(id,booking_id,kind,lead_seconds,due_at)
SELECT sqlc.arg(id)::uuid,b.id,sqlc.arg(kind)::text,sqlc.arg(lead_seconds)::integer,
 b.starts_at-sqlc.arg(lead_seconds)::integer*interval '1 second'
FROM shuttle_massage_bookings b WHERE b.id=sqlc.arg(booking_id)::uuid;

-- name: LockSchedulableMassageReminders :many
SELECT id FROM shuttle_massage_reminders
WHERE state IN ('pending','retryable') AND
 (sqlc.arg(booking_id)::uuid='00000000-0000-0000-0000-000000000000' OR booking_id=sqlc.arg(booking_id)::uuid)
ORDER BY id FOR UPDATE;

-- name: BlockBookingMassageReminders :exec
UPDATE shuttle_massage_reminders SET state='delivery_blocked',version=version+1
WHERE booking_id=$1 AND state IN ('pending','retryable');

-- name: RescheduleMassageReminders :exec
UPDATE shuttle_massage_reminders n SET
 lead_seconds=CASE WHEN n.kind='owner_long' THEN sqlc.arg(long_seconds)::integer ELSE sqlc.arg(short_seconds)::integer END,
 due_at=b.starts_at-(CASE WHEN n.kind='owner_long' THEN sqlc.arg(long_seconds)::integer ELSE sqlc.arg(short_seconds)::integer END)*interval '1 second',
 version=n.version+1
FROM shuttle_massage_bookings b WHERE n.booking_id=b.id AND n.state IN ('pending','retryable')
AND n.lead_seconds<>(CASE WHEN n.kind='owner_long' THEN sqlc.arg(long_seconds)::integer ELSE sqlc.arg(short_seconds)::integer END);

-- name: ReadMassageReminder :one
SELECT sqlc.embed(n),sqlc.embed(b) FROM shuttle_massage_reminders n
JOIN shuttle_massage_bookings b ON b.id=n.booking_id JOIN shuttle_events e ON e.id=b.event_id
WHERE n.id=sqlc.arg(id)::uuid AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id);

-- name: ReadyMassageReminders :many
SELECT n.id,n.version FROM shuttle_massage_reminders n
JOIN shuttle_massage_bookings b ON b.id=n.booking_id JOIN shuttle_events e ON e.id=b.event_id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND n.state IN ('pending','retryable') AND n.due_at<=clock_timestamp() AND n.next_attempt_at<=clock_timestamp()
AND n.id>sqlc.arg(after_id)::uuid ORDER BY n.id LIMIT sqlc.arg(page_limit);

-- name: MassageReminderRecipient :one
SELECT u.* FROM shuttle_massage_reminders n
JOIN shuttle_massage_bookings b ON b.id=n.booking_id JOIN shuttle_events e ON e.id=b.event_id
JOIN shuttle_users u ON u.id=b.user_id
WHERE n.id=sqlc.arg(id)::uuid AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'massage'=ANY(e.modules) AND b.state='booked' AND NOT u.revoked AND u.zitadel_subject<>''
AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id;

-- name: LockMassageReminder :one
SELECT * FROM shuttle_massage_reminders WHERE id=$1 FOR UPDATE;

-- name: ReserveMassageReminder :one
UPDATE shuttle_massage_reminders SET state='send_inflight',version=version+1,attempt_id=$3,payload=$4
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable')
AND due_at<=clock_timestamp() AND next_attempt_at<=clock_timestamp() RETURNING *;

-- name: FinishMassageReminder :execrows
UPDATE shuttle_massage_reminders SET state=sqlc.arg(state),version=version+1,receipt=sqlc.arg(receipt),
next_attempt_at=clock_timestamp()+sqlc.arg(delay_ms)::bigint*interval '1 millisecond',
lead_seconds=CASE WHEN sqlc.arg(state)::text='retryable' THEN sqlc.arg(lead_seconds)::integer ELSE lead_seconds END,
due_at=CASE WHEN sqlc.arg(state)::text='retryable' THEN sqlc.arg(due_at)::timestamptz ELSE due_at END
WHERE id=sqlc.arg(id)::uuid AND attempt_id=sqlc.arg(attempt_id)::uuid AND state='send_inflight';

-- name: BlockMassageReminder :execrows
UPDATE shuttle_massage_reminders SET state='delivery_blocked',version=version+1
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable');

-- name: RecoverMassageReminders :execrows
UPDATE shuttle_massage_reminders n SET state='delivery_unknown',version=n.version+1
FROM shuttle_massage_bookings b,shuttle_events e
WHERE b.id=n.booking_id AND e.id=b.event_id AND e.bot_id=$1 AND e.organization_id=$2 AND n.state='send_inflight';

-- name: ListAdminMassageReminders :many
SELECT n.id,n.booking_id,b.event_id,b.user_id AS recipient_id,n.kind,n.lead_seconds,n.due_at,n.next_attempt_at,n.state,n.version,n.created_at
FROM shuttle_massage_reminders n JOIN shuttle_massage_bookings b ON b.id=n.booking_id
JOIN shuttle_events e ON e.id=b.event_id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND (sqlc.arg(filter_state)::text='' OR n.state=sqlc.arg(filter_state))
AND n.id>sqlc.arg(after_id)::uuid ORDER BY n.id LIMIT sqlc.arg(page_limit);

-- name: ReadMassageBookingByID :one
SELECT * FROM shuttle_massage_bookings WHERE id=$1;

-- name: ResolveMassageReminder :one
UPDATE shuttle_massage_reminders SET state=sqlc.arg(state),version=version+1,next_attempt_at=clock_timestamp(),
lead_seconds=CASE WHEN sqlc.arg(state)::text='pending' THEN sqlc.arg(lead_seconds)::integer ELSE lead_seconds END,
due_at=CASE WHEN sqlc.arg(state)::text='pending' THEN sqlc.arg(due_at)::timestamptz ELSE due_at END
WHERE id=sqlc.arg(id)::uuid AND version=sqlc.arg(version)::bigint
AND state IN ('delivery_unknown','delivery_failed','delivery_blocked') RETURNING *;


-- name: ReadMassageSpecialistBinding :one
SELECT * FROM shuttle_massage_specialist_bindings WHERE event_id=$1 AND specialist_id=$2;

-- name: ListMassageSpecialistBindings :many
SELECT * FROM shuttle_massage_specialist_bindings
WHERE event_id=$1 AND specialist_id>sqlc.arg(after_id)::text
ORDER BY specialist_id LIMIT sqlc.arg(page_limit);

-- name: InsertMassageSpecialistBinding :one
INSERT INTO shuttle_massage_specialist_bindings(event_id,specialist_id,user_id,active,updated_by)
VALUES($1,$2,$3,$4,$5) RETURNING *;

-- name: UpdateMassageSpecialistBinding :one
UPDATE shuttle_massage_specialist_bindings
SET active=sqlc.arg(active),version=version+1,updated_by=sqlc.arg(actor_id),updated_at=clock_timestamp()
WHERE event_id=sqlc.arg(event_id) AND specialist_id=sqlc.arg(specialist_id)
AND user_id=sqlc.arg(user_id) AND version=sqlc.arg(expected_version)::bigint RETURNING *;

-- name: DeactivateRemovedMassageSpecialists :execrows
UPDATE shuttle_massage_specialist_bindings
SET active=false,version=version+1,updated_by=sqlc.arg(actor_id),updated_at=clock_timestamp()
WHERE event_id=sqlc.arg(event_id) AND active
AND NOT(specialist_id=ANY(sqlc.arg(retained_ids)::text[]));

-- name: OwnActiveMassageSpecialistIDs :many
SELECT specialist_id FROM shuttle_massage_specialist_bindings
WHERE event_id=$1 AND user_id=$2 AND active ORDER BY specialist_id;

-- name: ReadMassageBindingOperation :one
SELECT o.* FROM shuttle_massage_binding_operation_aliases a
JOIN shuttle_massage_binding_operations o
 ON o.actor_id=a.actor_id AND o.operation_id=a.canonical_operation_id
WHERE a.actor_id=$1 AND a.operation_id=$2;

-- name: ReadMassageBindingProposal :one
SELECT * FROM shuttle_massage_binding_operations WHERE actor_id=$1 AND proposal_id=$2;

-- name: ListPendingMassageBindingProposals :many
SELECT o.* FROM shuttle_massage_binding_operations o
WHERE o.actor_id=sqlc.arg(actor_id)
AND (sqlc.arg(event_id)::uuid='00000000-0000-0000-0000-000000000000' OR o.event_id=sqlc.arg(event_id)::uuid)
AND o.result IS NULL AND o.expires_at>clock_timestamp()
AND (sqlc.arg(after_id)::uuid='00000000-0000-0000-0000-000000000000'
 OR (o.created_at,o.proposal_id)>(SELECT c.created_at,c.proposal_id FROM shuttle_massage_binding_operations c
 WHERE c.actor_id=sqlc.arg(actor_id) AND c.proposal_id=sqlc.arg(after_id)::uuid))
ORDER BY o.created_at,o.proposal_id LIMIT 21;

-- name: InsertMassageBindingOperationAlias :exec
INSERT INTO shuttle_massage_binding_operation_aliases(actor_id,operation_id,canonical_operation_id)
VALUES($1,$2,$3);

-- name: InsertMassageBindingOperation :one
INSERT INTO shuttle_massage_binding_operations(actor_id,operation_id,proposal_id,event_id,specialist_id,request_hash,arguments)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: SaveMassageBindingOperation :execrows
UPDATE shuttle_massage_binding_operations SET result=$3
WHERE actor_id=$1 AND operation_id=$2 AND result IS NULL;


-- name: ListMassageStaffEvents :many
SELECT e.* FROM shuttle_events e
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND e.id>sqlc.arg(after_id)::uuid
AND EXISTS(SELECT 1 FROM shuttle_massage_specialist_bindings a
 WHERE a.event_id=e.id AND a.user_id=sqlc.arg(user_id) AND a.active)
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: ListMassageStaffBookings :many
SELECT sqlc.embed(b),
 left(coalesce(nullif(btrim(p.full_name),''),
 nullif(btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name')),''),''),256)::text AS client_name,
 CASE WHEN b.specialist_id=ANY(sqlc.arg(own_ids)::text[]) THEN u.tg_user_id ELSE 0 END::bigint AS contact_id
FROM shuttle_massage_bookings b
JOIN shuttle_events e ON e.id=b.event_id
JOIN shuttle_users u ON u.id=b.user_id AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id
LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
WHERE b.event_id=sqlc.arg(event_id)::uuid AND b.state='booked'
AND (NOT sqlc.arg(own_only)::boolean OR b.specialist_id=ANY(sqlc.arg(own_ids)::text[]))
AND (sqlc.arg(booking_id)::uuid='00000000-0000-0000-0000-000000000000' OR b.id=sqlc.arg(booking_id)::uuid)
AND (sqlc.arg(window_id)::text='' OR b.window_id=sqlc.arg(window_id)::text)
AND b.starts_at<sqlc.arg(until_at)::timestamptz AND b.occupancy_ends_at>sqlc.arg(from_at)::timestamptz
AND (sqlc.arg(after_id)::uuid='00000000-0000-0000-0000-000000000000'
 OR (b.starts_at,b.id)>(sqlc.arg(after_at)::timestamptz,sqlc.arg(after_id)::uuid))
ORDER BY b.starts_at,b.id LIMIT sqlc.arg(page_limit);

-- name: MassageScheduleVersion :one
SELECT version FROM shuttle_massage_schedules WHERE event_id=$1;


-- name: ReadMassageBindingTarget :one
SELECT u.id,u.tg_user_id,u.revoked,(u.zitadel_subject<>'')::boolean AS linked,
 left(coalesce(nullif(btrim(p.full_name),''),
 nullif(btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name')),''),''),256)::text AS display_name
FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
WHERE u.bot_id=sqlc.arg(bot_id) AND u.zitadel_org=sqlc.arg(organization_id)
AND ((sqlc.arg(user_id)::uuid<>'00000000-0000-0000-0000-000000000000' AND u.id=sqlc.arg(user_id)::uuid)
 OR (sqlc.arg(telegram_user_id)::bigint>0 AND u.tg_user_id=sqlc.arg(telegram_user_id)::bigint));


-- name: ListMassageBindingEvents :many
SELECT e.* FROM shuttle_events e
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND e.id>sqlc.arg(after_id)::uuid
AND EXISTS(SELECT 1 FROM shuttle_massage_schedules s WHERE s.event_id=e.id)
ORDER BY e.id LIMIT sqlc.arg(page_limit);

-- name: HasTaskMassageBindingContext :one
SELECT EXISTS(SELECT 1 FROM shuttle_tasks t
JOIN shuttle_massage_binding_operations o ON o.actor_id=t.user_id
JOIN shuttle_events e ON e.id=o.event_id
WHERE t.id=sqlc.arg(task_id)::uuid AND t.user_id=sqlc.arg(actor_id)::uuid
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND o.created_at<t.created_at
AND ((o.result IS NULL AND o.expires_at>clock_timestamp()) OR
 (o.result IS NOT NULL AND (o.result->>'updated_at')::timestamptz<t.created_at)));

-- name: ListTaskPendingMassageBindings :many
SELECT o.* FROM shuttle_tasks t
JOIN shuttle_massage_binding_operations o ON o.actor_id=t.user_id
JOIN shuttle_events e ON e.id=o.event_id
WHERE t.id=sqlc.arg(task_id)::uuid AND t.user_id=sqlc.arg(actor_id)::uuid
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND o.created_at<t.created_at AND o.result IS NULL AND o.expires_at>clock_timestamp()
ORDER BY o.created_at DESC,o.proposal_id DESC LIMIT 5;

-- name: ListTaskCompletedMassageBindings :many
SELECT o.* FROM shuttle_tasks t
JOIN shuttle_massage_binding_operations o ON o.actor_id=t.user_id
JOIN shuttle_events e ON e.id=o.event_id
WHERE t.id=sqlc.arg(task_id)::uuid AND t.user_id=sqlc.arg(actor_id)::uuid
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND o.created_at<t.created_at AND o.result IS NOT NULL
AND (o.result->>'updated_at')::timestamptz<t.created_at
ORDER BY (o.result->>'updated_at')::timestamptz DESC,o.proposal_id DESC LIMIT 5;


-- name: ListMassageStaffWindows :many
WITH candidates AS (
 SELECT w->>'id' AS window_id, (w->>'starts_at')::timestamptz AS starts_at,
 (w->>'ends_at')::timestamptz AS ends_at, w->'name' AS name, 0 AS priority,
 '00000000-0000-0000-0000-000000000000'::uuid AS booking_id
 FROM shuttle_massage_schedules s,
 jsonb_array_elements(s.document->'windows') w WHERE s.event_id=sqlc.arg(event_id)::uuid
 UNION ALL
 SELECT b.window_id, (b.snapshot->'terms'->>'window_starts_at')::timestamptz,
 (b.snapshot->'terms'->>'window_ends_at')::timestamptz,
 b.snapshot->'terms'->'window_name', 1, b.id
 FROM shuttle_massage_bookings b WHERE b.event_id=sqlc.arg(event_id)::uuid AND b.state='booked'
), windows AS (
 SELECT DISTINCT ON (starts_at,ends_at,window_id COLLATE "C") window_id,starts_at,ends_at,name
 FROM candidates ORDER BY starts_at,ends_at,window_id COLLATE "C",priority,booking_id
)
SELECT window_id::text,starts_at::timestamptz,ends_at::timestamptz,name::jsonb
FROM windows
WHERE NOT sqlc.arg(has_after)::boolean OR
 (starts_at,ends_at,window_id COLLATE "C")>
 (sqlc.arg(after_starts_at)::timestamptz,sqlc.arg(after_ends_at)::timestamptz,sqlc.arg(after_window_id)::text COLLATE "C")
ORDER BY starts_at,ends_at,window_id COLLATE "C" LIMIT 21;

-- name: ReadMassageSpecialistPreference :one
SELECT coalesce(p.notify_bookings,true)::boolean AS notify_bookings,
 coalesce(p.notify_next,true)::boolean AS notify_next, coalesce(p.version,0)::bigint AS version
FROM shuttle_users u LEFT JOIN shuttle_massage_specialist_preferences p ON p.user_id=u.id
WHERE u.id=$1;

-- name: SetMassageSpecialistPreference :one
INSERT INTO shuttle_massage_specialist_preferences
 (user_id,notify_bookings,notify_next,version,notify_bookings_version,notify_next_version)
VALUES(sqlc.arg(user_id),sqlc.arg(notify_bookings)::boolean,sqlc.arg(notify_next)::boolean,sqlc.arg(next_version)::bigint,
 CASE WHEN sqlc.arg(notify_bookings)::boolean THEN 0 ELSE sqlc.arg(next_version)::bigint END,
 CASE WHEN sqlc.arg(notify_next)::boolean THEN 0 ELSE sqlc.arg(next_version)::bigint END)
ON CONFLICT(user_id) DO UPDATE SET
 notify_bookings=EXCLUDED.notify_bookings,notify_next=EXCLUDED.notify_next,version=EXCLUDED.version,
 notify_bookings_version=CASE WHEN shuttle_massage_specialist_preferences.notify_bookings<>EXCLUDED.notify_bookings
 THEN EXCLUDED.version ELSE shuttle_massage_specialist_preferences.notify_bookings_version END,
 notify_next_version=CASE WHEN shuttle_massage_specialist_preferences.notify_next<>EXCLUDED.notify_next
 THEN EXCLUDED.version ELSE shuttle_massage_specialist_preferences.notify_next_version END
WHERE shuttle_massage_specialist_preferences.version=sqlc.arg(expected_version)
RETURNING *;

-- name: ReadMassageSpecialistPreferenceOperation :one
SELECT * FROM shuttle_massage_specialist_preference_operations WHERE user_id=$1 AND operation_id=$2;

-- name: InsertMassageSpecialistPreferenceOperation :exec
INSERT INTO shuttle_massage_specialist_preference_operations(user_id,operation_id,event_id,source,request_hash,result)
VALUES($1,$2,$3,$4,$5,$6);

-- name: InsertMassageSpecialistNotification :exec
INSERT INTO shuttle_massage_specialist_notifications
 (id,booking_id,kind,recipient_id,binding_version,preference_version,client_name)
SELECT sqlc.arg(id)::uuid,b.id,sqlc.arg(kind)::text,s.user_id,s.version,coalesce(p.notify_bookings_version,0),
 left(coalesce(nullif(btrim(client_profile.full_name),''),
 nullif(btrim(concat_ws(' ',client.profile->>'first_name',client.profile->>'last_name')),''),''),256)
FROM shuttle_massage_bookings b
JOIN shuttle_events e ON e.id=b.event_id AND 'massage'=ANY(e.modules)
JOIN shuttle_massage_specialist_bindings s ON s.event_id=b.event_id AND s.specialist_id=b.specialist_id AND s.active
JOIN shuttle_users recipient ON recipient.id=s.user_id AND recipient.bot_id=e.bot_id
 AND recipient.zitadel_org=e.organization_id AND recipient.zitadel_subject<>'' AND NOT recipient.revoked
JOIN shuttle_users client ON client.id=b.user_id AND client.bot_id=e.bot_id AND client.zitadel_org=e.organization_id
LEFT JOIN shuttle_user_profiles client_profile ON client_profile.user_id=client.id
LEFT JOIN shuttle_massage_specialist_preferences p ON p.user_id=s.user_id
WHERE b.id=sqlc.arg(booking_id)::uuid AND coalesce(p.notify_bookings,true)
AND ((sqlc.arg(kind)::text='booked' AND b.state='booked') OR
 (sqlc.arg(kind)::text='cancelled' AND b.state='cancelled'))
ON CONFLICT(booking_id,kind) DO NOTHING;

-- name: BlockBookedMassageSpecialistNotification :exec
UPDATE shuttle_massage_specialist_notifications SET state='delivery_blocked',version=version+1
WHERE booking_id=$1 AND kind IN ('booked','soon') AND state IN ('pending','retryable');

-- name: ReadMassageSpecialistNotification :one
SELECT sqlc.embed(n),sqlc.embed(b) FROM shuttle_massage_specialist_notifications n
JOIN shuttle_massage_bookings b ON b.id=n.booking_id JOIN shuttle_events e ON e.id=b.event_id
WHERE n.id=sqlc.arg(id)::uuid AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id);

-- name: ReadyMassageSpecialistNotifications :many
SELECT n.id,n.version FROM shuttle_massage_specialist_notifications n
JOIN shuttle_massage_bookings b ON b.id=n.booking_id JOIN shuttle_events e ON e.id=b.event_id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND n.state IN ('pending','retryable') AND n.next_attempt_at<=clock_timestamp()
AND n.id>sqlc.arg(after_id)::uuid ORDER BY n.id LIMIT sqlc.arg(page_limit);

-- name: MassageSpecialistNotificationRecipient :one
SELECT u.* FROM shuttle_massage_specialist_notifications n
JOIN shuttle_massage_bookings b ON b.id=n.booking_id JOIN shuttle_events e ON e.id=b.event_id
JOIN shuttle_users u ON u.id=n.recipient_id AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id
JOIN shuttle_massage_specialist_bindings s ON s.event_id=b.event_id AND s.specialist_id=b.specialist_id
 AND s.user_id=n.recipient_id AND s.active AND s.version=n.binding_version
LEFT JOIN shuttle_massage_specialist_preferences p ON p.user_id=u.id
WHERE n.id=sqlc.arg(id)::uuid AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'massage'=ANY(e.modules) AND NOT u.revoked AND u.zitadel_subject<>''
AND ((n.kind IN ('booked','cancelled') AND coalesce(p.notify_bookings,true)
 AND coalesce(p.notify_bookings_version,0)=n.preference_version
 AND ((n.kind='booked' AND b.state='booked') OR (n.kind='cancelled' AND b.state='cancelled')))
 OR (n.kind='soon' AND b.state='booked' AND e.ends_at>clock_timestamp()
 AND coalesce(p.notify_next,true) AND coalesce(p.notify_next_version,0)=n.preference_version));

-- name: ReserveMassageSpecialistNotification :one
UPDATE shuttle_massage_specialist_notifications SET state='send_inflight',version=version+1,attempt_id=$3,payload=$4
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable') AND next_attempt_at<=clock_timestamp() RETURNING *;

-- name: FinishMassageSpecialistNotification :execrows
UPDATE shuttle_massage_specialist_notifications SET state=sqlc.arg(state),version=version+1,receipt=sqlc.arg(receipt),
next_attempt_at=clock_timestamp()+sqlc.arg(delay_ms)::bigint*interval '1 millisecond'
WHERE id=sqlc.arg(id)::uuid AND attempt_id=sqlc.arg(attempt_id)::uuid AND state='send_inflight';

-- name: BlockMassageSpecialistNotification :execrows
UPDATE shuttle_massage_specialist_notifications SET state='delivery_blocked',version=version+1
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable');

-- name: RecoverMassageSpecialistNotifications :execrows
UPDATE shuttle_massage_specialist_notifications n SET state='delivery_unknown',version=n.version+1
FROM shuttle_massage_bookings b,shuttle_events e
WHERE b.id=n.booking_id AND e.id=b.event_id AND e.bot_id=$1 AND e.organization_id=$2 AND n.state='send_inflight';

-- name: ListAdminMassageSpecialistNotifications :many
SELECT n.id,n.booking_id,b.event_id,n.recipient_id,n.kind,n.state,n.version,n.binding_version,n.preference_version,n.next_attempt_at,n.created_at
FROM shuttle_massage_specialist_notifications n JOIN shuttle_massage_bookings b ON b.id=n.booking_id
JOIN shuttle_events e ON e.id=b.event_id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND (sqlc.arg(filter_state)::text='' OR n.state=sqlc.arg(filter_state))
AND n.id>sqlc.arg(after_id)::uuid ORDER BY n.id LIMIT sqlc.arg(page_limit);

-- name: ResolveMassageSpecialistNotification :one
UPDATE shuttle_massage_specialist_notifications SET state=sqlc.arg(state),version=version+1,next_attempt_at=clock_timestamp()
WHERE id=sqlc.arg(id)::uuid AND version=sqlc.arg(version)::bigint
AND state IN ('delivery_unknown','delivery_failed','delivery_blocked') RETURNING *;

-- name: ListTaskMassageSpecialistPreferenceOperations :many
SELECT op.* FROM shuttle_tasks task
JOIN shuttle_massage_specialist_preference_operations op ON op.user_id=task.user_id
JOIN shuttle_events e ON e.id=op.event_id
WHERE task.id=sqlc.arg(task_id)::uuid AND task.user_id=sqlc.arg(user_id)::uuid
AND e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'massage'=ANY(e.modules)
AND EXISTS(SELECT 1 FROM shuttle_massage_specialist_bindings b
 WHERE b.event_id=e.id AND b.user_id=task.user_id AND b.active)
AND op.created_at<task.created_at
ORDER BY op.created_at DESC,op.operation_id LIMIT 5;


-- name: DueMassageSpecialistSoonBookings :many
SELECT b.id FROM shuttle_massage_bookings b
JOIN shuttle_events e ON e.id=b.event_id
JOIN shuttle_massage_specialist_bindings s ON s.event_id=b.event_id AND s.specialist_id=b.specialist_id AND s.active
JOIN shuttle_users u ON u.id=s.user_id AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id
WHERE e.bot_id=sqlc.arg(bot_id) AND e.organization_id=sqlc.arg(organization_id)
AND 'massage'=ANY(e.modules) AND e.ends_at>clock_timestamp()
AND b.state='booked' AND b.starts_at<=clock_timestamp()+interval '5 minutes'
AND NOT u.revoked AND u.zitadel_subject<>''
AND NOT EXISTS(SELECT 1 FROM shuttle_massage_specialist_notifications n WHERE n.booking_id=b.id AND n.kind='soon')
ORDER BY b.starts_at,b.id LIMIT sqlc.arg(page_limit);

-- name: InsertMassageSpecialistSoonNotification :exec
INSERT INTO shuttle_massage_specialist_notifications
(id,booking_id,kind,recipient_id,binding_version,preference_version,client_name,state)
SELECT sqlc.arg(id)::uuid,b.id,'soon',s.user_id,s.version,coalesce(p.notify_next_version,0),
 left(coalesce(nullif(btrim(client_profile.full_name),''),
 nullif(btrim(concat_ws(' ',client.profile->>'first_name',client.profile->>'last_name')),''),''),256),
 CASE WHEN coalesce(p.notify_next,true) THEN 'pending' ELSE 'delivery_blocked' END
FROM shuttle_massage_bookings b JOIN shuttle_events e ON e.id=b.event_id
JOIN shuttle_massage_specialist_bindings s ON s.event_id=b.event_id AND s.specialist_id=b.specialist_id AND s.active
JOIN shuttle_users recipient ON recipient.id=s.user_id AND recipient.bot_id=e.bot_id
 AND recipient.zitadel_org=e.organization_id AND recipient.zitadel_subject<>'' AND NOT recipient.revoked
JOIN shuttle_users client ON client.id=b.user_id AND client.bot_id=e.bot_id AND client.zitadel_org=e.organization_id
LEFT JOIN shuttle_user_profiles client_profile ON client_profile.user_id=client.id
LEFT JOIN shuttle_massage_specialist_preferences p ON p.user_id=s.user_id
WHERE b.id=sqlc.arg(booking_id)::uuid AND e.bot_id=sqlc.arg(bot_id)
AND e.organization_id=sqlc.arg(organization_id) AND 'massage'=ANY(e.modules)
AND e.ends_at>clock_timestamp() AND b.state='booked' AND b.starts_at<=clock_timestamp()+interval '5 minutes'
ON CONFLICT(booking_id,kind) DO NOTHING;


-- name: ReadMealCatalog :one
SELECT * FROM shuttle_meal_catalogs WHERE event_id=$1 ORDER BY version DESC LIMIT 1;
-- name: ReadMealCatalogVersion :one
SELECT * FROM shuttle_meal_catalogs WHERE event_id=$1 AND version=$2;
-- name: InsertMealCatalog :one
INSERT INTO shuttle_meal_catalogs(event_id,version,document) VALUES($1,$2,$3) RETURNING *;
-- name: ReadMealCatalogPreparation :one
SELECT * FROM shuttle_meal_catalog_preparations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertMealCatalogPreparation :one
INSERT INTO shuttle_meal_catalog_preparations(id,user_id,operation_id,event_id,request_hash,arguments,expires_at)
VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+interval '5 minutes') RETURNING *;
-- name: ReadMealCatalogOperation :one
SELECT event_id,request_hash,result FROM shuttle_meal_catalog_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertMealCatalogOperation :exec
INSERT INTO shuttle_meal_catalog_operations(user_id,operation_id,event_id,request_hash,result) VALUES($1,$2,$3,$4,$5);
-- name: ReadMealDraft :one
SELECT result FROM shuttle_meal_drafts WHERE user_id=$1 AND event_id=$2;
-- name: SaveMealDraft :exec
INSERT INTO shuttle_meal_drafts(user_id,event_id,catalog_version,version,result) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(user_id,event_id) DO UPDATE SET catalog_version=EXCLUDED.catalog_version,version=EXCLUDED.version,result=EXCLUDED.result;
-- name: ReadMealDraftOperation :one
SELECT event_id,request_hash,result FROM shuttle_meal_draft_operations WHERE user_id=$1 AND operation_id=$2;
-- name: InsertMealDraftOperation :exec
INSERT INTO shuttle_meal_draft_operations(user_id,operation_id,event_id,catalog_version,request_hash,result) VALUES($1,$2,$3,$4,$5,$6);
-- name: InsertMiniAppMealDraftAction :exec
INSERT INTO shuttle_miniapp_meal_draft_actions(user_id,operation_id) VALUES($1,$2);

-- name: ListOwnMealDraftEvents :many
SELECT e.* FROM shuttle_events e
JOIN shuttle_registrations r ON r.event_id=e.id AND r.user_id=$1 AND r.state='assigned'
JOIN shuttle_passes p ON p.registration_id=r.id
WHERE e.bot_id=$2 AND e.organization_id=$3 AND 'meals'=ANY(e.modules) AND e.id>$4
ORDER BY e.id LIMIT $5;

-- name: RecentMiniAppMealDraftActions :many
SELECT a.operation_id,op.event_id,(op.result->>'version')::bigint AS draft_version,
 (op.result->>'event_version')::bigint AS event_version,
 (op.result->>'catalog_version')::bigint AS catalog_version,
 (op.result->>'cleared')::boolean AS cleared,a.completed_at
 FROM shuttle_tasks task JOIN shuttle_miniapp_meal_draft_actions a ON a.user_id=task.user_id
 JOIN shuttle_meal_draft_operations op ON op.user_id=a.user_id AND op.operation_id=a.operation_id
 JOIN shuttle_events e ON e.id=op.event_id
 JOIN shuttle_registrations r ON r.event_id=e.id AND r.user_id=a.user_id AND r.state='assigned'
 JOIN shuttle_passes p ON p.registration_id=r.id
 WHERE task.id=$1 AND task.user_id=$2 AND a.completed_at<task.created_at
 AND e.bot_id=$3 AND e.organization_id=$4 AND 'meals'=ANY(e.modules)
 ORDER BY a.completed_at DESC,a.operation_id LIMIT 5;

-- name: GetAvatarDelivery :one
SELECT n.* FROM shuttle_avatar_deliveries n JOIN shuttle_users u ON u.id=n.user_id
WHERE n.id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3;
-- name: ReadyAvatarDeliveries :many
SELECT n.id,n.version FROM shuttle_avatar_deliveries n JOIN shuttle_users u ON u.id=n.user_id
WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND n.state IN ('pending','retryable')
AND n.next_attempt_at<=clock_timestamp() AND n.id>sqlc.arg(after_id)::uuid
ORDER BY n.id LIMIT sqlc.arg(page_limit);
-- name: AvatarDeliveryRecipient :one
SELECT u.* FROM shuttle_avatar_deliveries n JOIN shuttle_users u ON u.id=n.user_id
WHERE n.id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3 AND NOT u.revoked
AND u.zitadel_subject<>'' AND u.tg_user_id=n.telegram_user_id;
-- name: ReserveAvatarDelivery :one
UPDATE shuttle_avatar_deliveries SET state='send_inflight',version=version+1,attempt_id=$3
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable')
AND next_attempt_at<=clock_timestamp() RETURNING *;
-- name: FinishAvatarDelivery :execrows
UPDATE shuttle_avatar_deliveries SET state=sqlc.arg(outcome),version=version+1,receipt=sqlc.arg(receipt),
next_attempt_at=clock_timestamp()+sqlc.arg(delay_ms)::bigint*interval '1 millisecond'
WHERE id=$1 AND version=sqlc.arg(expected_version) AND attempt_id=sqlc.arg(attempt_id) AND state='send_inflight';
-- name: BlockAvatarDelivery :execrows
UPDATE shuttle_avatar_deliveries SET state='delivery_blocked',version=version+1
WHERE id=$1 AND version=$2 AND state IN ('pending','retryable');
-- name: RecoverAvatarDeliveries :execrows
UPDATE shuttle_avatar_deliveries n SET state='delivery_unknown',version=n.version+1
FROM shuttle_users u WHERE u.id=n.user_id AND u.bot_id=$1 AND u.zitadel_org=$2 AND n.state='send_inflight';

-- name: GetAvatarDeliveryResolution :one
SELECT r.* FROM shuttle_avatar_delivery_resolutions r
WHERE r.bot_id=$1 AND r.organization_id=$2 AND r.actor=$3
AND (r.operation_id=$4 OR r.operation_id=(SELECT a.canonical_operation_id FROM shuttle_avatar_resolution_aliases a
 WHERE a.bot_id=$1 AND a.organization_id=$2 AND a.actor=$3 AND a.operation_id=$4));
-- name: InsertAvatarDeliveryResolution :exec
INSERT INTO shuttle_avatar_delivery_resolutions(bot_id,organization_id,actor,operation_id,proposal_id,delivery_id,request_hash,expires_at,document)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: SaveAvatarDeliveryResolution :execrows
UPDATE shuttle_avatar_delivery_resolutions SET result=$5
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 AND result IS NULL;
-- name: LockAvatarDeliveryResolutionOwner :one
SELECT id FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3 FOR SHARE;
-- name: LockAvatarDeliveryResolutionTarget :one
SELECT n.* FROM shuttle_avatar_deliveries n JOIN shuttle_users u ON u.id=n.user_id
WHERE n.id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3 FOR UPDATE OF n;
-- name: ApplyAvatarDeliveryResolution :one
UPDATE shuttle_avatar_deliveries SET state=sqlc.arg(outcome),version=version+1,attempt_id=NULL,
 receipt='{}',next_attempt_at=clock_timestamp()
WHERE id=$1 AND version=sqlc.arg(expected_version)
AND state IN ('delivery_unknown','delivery_failed','delivery_blocked') RETURNING *;

-- name: GetAvatarDeliveryProposal :one
SELECT r.* FROM shuttle_avatar_delivery_resolutions r
WHERE r.bot_id=sqlc.arg(bot_id) AND r.organization_id=sqlc.arg(organization_id) AND r.actor=sqlc.arg(actor)
AND ((sqlc.arg(operation_id)::uuid <> '00000000-0000-0000-0000-000000000000'
 AND (r.operation_id=sqlc.arg(operation_id) OR r.operation_id=(SELECT a.canonical_operation_id FROM shuttle_avatar_resolution_aliases a
 WHERE a.bot_id=sqlc.arg(bot_id) AND a.organization_id=sqlc.arg(organization_id) AND a.actor=sqlc.arg(actor) AND a.operation_id=sqlc.arg(operation_id)))) OR
 (sqlc.arg(proposal_id)::uuid <> '00000000-0000-0000-0000-000000000000' AND r.proposal_id=sqlc.arg(proposal_id)));
-- name: ListPendingAvatarDeliveryProposals :many
SELECT * FROM shuttle_avatar_delivery_resolutions
WHERE bot_id=sqlc.arg(bot_id) AND organization_id=sqlc.arg(organization_id) AND actor=sqlc.arg(actor)
AND proposal_id > sqlc.arg(after_id)::uuid AND result IS NULL AND expires_at > sqlc.arg(now_at)::timestamptz
ORDER BY proposal_id LIMIT sqlc.arg(page_limit);

-- name: LockAvatarDeliveryCanonicalResolution :one
SELECT * FROM shuttle_avatar_delivery_resolutions
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 FOR UPDATE;
-- name: LockAvatarDeliveryResolutionProposal :one
SELECT * FROM shuttle_avatar_delivery_resolutions
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND proposal_id=$4 FOR UPDATE;
-- name: InsertAvatarDeliveryResolutionAlias :exec
INSERT INTO shuttle_avatar_resolution_aliases(bot_id,organization_id,actor,operation_id,canonical_operation_id)
VALUES($1,$2,$3,$4,$5);

-- name: InsertNativeAvatarResolutionAction :exec
INSERT INTO shuttle_miniapp_avatar_resolution_actions(user_id,operation_id,proposal_id,kind,delivery_version)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,operation_id,kind) DO NOTHING;
-- name: HasNativeAvatarResolutionContext :one
SELECT EXISTS(SELECT 1 FROM shuttle_miniapp_avatar_resolution_actions a
JOIN shuttle_avatar_delivery_resolutions r ON r.proposal_id=a.proposal_id
JOIN shuttle_tasks t ON t.user_id=a.user_id
WHERE a.user_id=sqlc.arg(user_id) AND t.id=sqlc.arg(task_id)
AND r.bot_id=sqlc.arg(bot_id) AND r.organization_id=sqlc.arg(organization_id) AND r.actor=sqlc.arg(actor)
AND a.created_at<t.created_at);
-- name: RecentNativeAvatarResolutionActions :many
SELECT a.operation_id,a.proposal_id,a.kind,a.delivery_version,a.created_at,
r.operation_id AS canonical_operation_id,r.delivery_id
FROM shuttle_miniapp_avatar_resolution_actions a
JOIN shuttle_avatar_delivery_resolutions r ON r.proposal_id=a.proposal_id
JOIN shuttle_tasks t ON t.user_id=a.user_id
WHERE a.user_id=sqlc.arg(user_id) AND t.id=sqlc.arg(task_id)
AND r.bot_id=sqlc.arg(bot_id) AND r.organization_id=sqlc.arg(organization_id) AND r.actor=sqlc.arg(actor)
AND a.created_at<t.created_at
ORDER BY a.created_at DESC,a.operation_id,a.kind LIMIT 5;

-- name: LockAssignmentCanonicalPreparation :one
SELECT * FROM shuttle_assignment_preparations
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND operation_id=$4 FOR UPDATE;
-- name: LockAssignmentProposal :one
SELECT * FROM shuttle_assignment_preparations
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND proposal_id=$4 FOR UPDATE;
-- name: InsertAssignmentAlias :exec
INSERT INTO shuttle_assignment_aliases(bot_id,organization_id,actor,operation_id,canonical_operation_id)
VALUES($1,$2,$3,$4,$5);

-- name: GetAssignmentProposal :one
SELECT * FROM shuttle_assignment_preparations
WHERE bot_id=$1 AND organization_id=$2 AND actor=$3 AND proposal_id=$4;
-- name: ListPendingAssignmentProposals :many
SELECT p.* FROM shuttle_assignment_preparations p
WHERE p.bot_id=sqlc.arg(bot_id) AND p.organization_id=sqlc.arg(organization_id) AND p.actor=sqlc.arg(actor)
AND p.proposal_id>sqlc.arg(after_id)::uuid AND p.expires_at>sqlc.arg(now_at)::timestamptz
AND NOT EXISTS(SELECT 1 FROM shuttle_registration_operations r
 WHERE r.bot_id=p.bot_id AND r.organization_id=p.organization_id AND r.actor=p.actor AND r.operation_id=p.operation_id)
ORDER BY p.proposal_id LIMIT sqlc.arg(page_limit);
