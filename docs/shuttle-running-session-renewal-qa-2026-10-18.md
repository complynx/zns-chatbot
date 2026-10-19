# Running-task session renewal QA — 2026-10-18

The actual bot renewed the original delegated Loom session before its initial
expiry while the original model request remained pending. The same task/run,
session ID and model step continued beyond the original expiry and completed
with one final outbox/send. No service restarted during the accepted gate.

## Actual path and time proof

Fresh `session-renewal-b` used pinned local `mvp-56`/schema49, frozen Loom fc6c574,
its own real PostgreSQL/Zitadel/Loom, CA, fake Telegram and counted mailbox.
The fixture changed only its own access/id token lifetimes to 90s through
Zitadel's admin API, preserving the original refresh-idle/absolute lifetimes.
All four fields were read back. Fake update 7501 created one real bot user,
inbox/task, delegated session at version 1, run and model request.

The model request stayed unanswered. Repeated observations checked a live Loom
lease, running original run, the same pending model-step ID, unchanged immutable
task identity and request, one session ID, no outbox/send and no business writes.
All nine service IDs/images/process epochs stayed pinned.

A higher session version with a later expiry was observed and persisted in both
Loom and the bot's task checkpoint 10.710130s before the original session expiry.
Version advanced from 1 to 2; expiry advanced 60s. This is receipt/state evidence
from real delegated authorization, not a separate capture or signature check of
the raw JWT. The frozen Loom contract derives session expiry from the verified
subject principal. No raw credential was printed or included in this report.

The original unanswered request remained pending 2.743183s beyond the original
expiry. The renewed session was still valid. Only then did the fixture publish
one exact manual final response. One original model step and run completed;
one outbox row was delivered with one fake-Telegram send attempt and visible
reply. There was no replacement run/session/model request, sponsored unknown
attempt or Loom `write_operations` row.

All four original OIDC settings were then restored and read back. This gate
accepts proactive renewal through original expiry; it does not force the current
session to expire or combine renewal with IAM downtime.

## Retained failed attempt and review

The first `session-renewal` attempt failed a QA assertion: it expected a model
step to be `running`, whereas Loom keeps that step `pending` during inference
and the run `running`. Read-only inspection established the actual contract.
No update or model answer was replayed. Its original OIDC settings were restored
with the cleanup path, its bot/Loom were stopped, private archives/logs retained,
and the exact disposable project retired. Its checkpoint remains unaccepted.

The corrected driver was reviewed independently for correctness and safety.
Earlier review fixes required evidence of renewal before expiry and provided
settings reconciliation after failed/uncertain phases without accepting a failed
gate. Fresh final reviews were clean. Fifteen source/config hashes and immutable
runtime image IDs were pinned. These are runtime checks of frozen images, not a
fresh image build, real Telegram transport or unattended-model acceptance.

## Preservation and cleanup

Across both attempts, primary bot/poller epochs and 132 Loom runs were unchanged.
All 112 fingerprints matched except River leader/queue bookkeeping; all 46 River
job rows were identical. No business drift or retention exemption was needed.
Both temporary projects have zero containers, volumes or networks. Settings were
restored before retirement. Credentials, original/final records, observations,
transcripts, review/source gates, logs and database archives remain ignored under
`.cache/shuttle-work/running-session-renewal-v1-qa` and
`.cache/shuttle-work/running-session-renewal-v2-qa`.

The accepted application archive is 298605 bytes, SHA-256
`cd06f38e1364c6205ef4e48d9061dba5ad2b43b3832656eea4045029a159a3fd`;
the accepted Loom archive is 90327 bytes, SHA-256
`6dd3a42ef075f8ae9c62309d2654100f1c4480abb06d153657dc153aa3365380`.

A later [running-session expiry/IAM outage gate](shuttle-session-fallback-outage-qa-2026-10-18.md)
found and fixed unnecessary same-expiry reauthorization, then passed on a fresh
Linux candidate. This report retains its original pre-expiry renewal scope.
Revocation during caching, uncertain reauthorization recovery, abrupt Loom kill
and production acceptance remain open. The separate [credential expiry helper](shuttle-iam-credential-expiry-qa-2026-10-18.md)
and [IAM-first recovery](shuttle-iam-first-continuous-qa-2026-10-18.md) prove their
own specified paths; they do not replace these remaining gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
