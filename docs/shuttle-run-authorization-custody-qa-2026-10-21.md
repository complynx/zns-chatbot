# Run custody through authorization downtime

A continuous Loom 401 could previously exhaust the five-minute retry budget,
mark the local task failed and release its conversation while the remote run
was still active. This also affected creation accepted remotely with a lost
response. `disconnect_policy: continue` permits that run to keep working.

The bot now retains ownership beyond the authorization deadline once a creation
request is prepared or a run ID is known. Restart preserves the original request,
session and idempotency key. New messages continue the retained task. The
five-minute denial policy still applies before creation is prepared. Current
credentials and permissions are checked on every attempt; this change grants no
access and does not change explicit local revocation or `permission_revoked`.

## Behavioral verification

On the original code, both the known-run and lost-creation-response cases became
`failed` after an injected expired deadline. That failure is retained.

With the fix, both cases survive a restart and retain `active_task_id` in the real
PostgreSQL conversation. A follow-up routes to that task. Restoring authorization
applies that input to the same remote run, with one run and the original creation
key. The waiting-auth renewal case also recovers the original session/run after
an expired deadline; the bootstrap-only case still denies after the deadline.

The final Linux/race gate passed 15 integration scenarios / 17 cases, with no
failures or skips. Adjacent cases cover local revocation, explicit permission
revocation, availability outages, lost renewal receipts and lost creation
receipts. The task/integration lint gate reported zero issues. Independent
source review passes v1 and v2 reported no actionable findings.

Two intermediate verification failures are retained: the first lint gate
required parallel subtests; the first integration gate found that the new test
tried to send a follow-up through an already stopped HTTP fixture. Both test
issues were fixed before the final gate. The original application failure is
separate from those verification errors.

The final runner used a 3 GiB limit, with no swap and no OOM kill. It used an
isolated PostgreSQL and HTTP IAM/Loom/Telegram peers. This accepts the durable
routing and recovery boundary; actual IAM/Loom and Telegram WebView acceptance
of this source still requires a separately installed image. The primary stand
was not upgraded, and production was not changed.

Local evidence: `.cache/shuttle-work/run-auth-custody-v1/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
