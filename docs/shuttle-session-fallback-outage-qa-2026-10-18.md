# Running-session expiry during an IAM outage

Accepted on 2026-10-18 on a fresh isolated mock-Telegram stand with real
Zitadel and Loom. This gate found and fixed a bot bug before acceptance.
At this gate, the primary `shuttle-live-test` bot retained its previous image.
The subsequent [owned primary installation](shuttle-session-fallback-primary-qa-2026-10-18.md)
accepted the candidate and an actual binary rollback separately. No production, real Telegram or Loom developer stand
was changed.

## Failure and fix

The original `session-outage` attempt used the previous bot image. During the
proactive renewal window, IAM was unavailable and the identity client correctly
returned a still-valid cached delegated credential. Its expiry did not extend the
existing Loom session. The bot nevertheless called the non-idempotent session
reauthorization endpoint. A 503 became an uncertain mutation outcome, leaving the
task in `auth_unknown` and delivering a premature administrator-check notice
about 15 seconds before the original session expired. That attempt remains
unaccepted; its records, logs and database archives were retained before cleanup.

`internal/task/engine.go` now skips this unnecessary proactive mutation while
both the existing session and credential remain usable and the credential does
not extend the session expiry. Explicit `waiting_auth/authorization_required`
recovery and expired sessions still follow the existing renewal path. Uncertain
outcomes from actual reauthorization mutations remain conservative; no retry
safety rule was weakened. No schema, dependency, configuration or API change was
needed. The manifest stays `mvp-56`; the image digest identifies this patch.

## Behavioral verification and build

`TestSameExpiryCredentialKeepsValidLoomSession` first failed on the previous
engine with the expected `auth_unknown`, then passed after the patch. It uses
public intake and observable run/session/delivery behavior. The mock models a
same-expiry credential and an unsafe renewal response; real IAM fallback is
covered by the separate runtime gate below.

Thirteen focused authorization/recovery tests passed on native Windows Go 1.27.2
with PostgreSQL, including required recovery with an unexpired session, lost
reauthorization receipts across restart, revocation, grace expiry and dependency
outage recovery. These were not race tests or a full-suite run. Scoped
`golangci-lint` passed. Its first attempt failed because its helper resolved
Go 1.27.0 from PATH; prepending the explicit Go 1.27.2 bin directory fixed that
environment error. The product diff passed two independent reviews.

The Linux binary was cross-compiled from frozen sources with CGO disabled. A
thin test image replaced only the application binary in the previous runtime
image; avatar/runtime assets were reused. Candidate image:
`sha256:46a9abfd65e3f7d9138f7e106a640997551fa5f191bbff428c3b1486c32c03eb`.
Binary SHA-256:
`8b9d243ca50cb1a2ec4d3f5bcd39fde6b9d64feac6039bd405615f114600f66f`.
This is local candidate validation, not acceptance of a fresh production image
build or deployment.

## Actual bot acceptance

The fresh `session-outage-b` stand ran the candidate with the frozen real
Zitadel/Loom images, private TLS manual provider and mock Telegram. All nine
container IDs, images, owners and process epochs, plus 21 source/config hashes,
were pinned. Preflight checked owned loopback IAM/mock port mappings, the bot
config mount, mock Telegram TLS endpoint and private provider alias. Two fresh
independent QA-driver reviews were clean after adding explicit endpoint binding
and proof that IAM stopped before the original receipt expired.

1. Set only the stand's access/ID token lifetimes to 90 seconds. Read back all
   four OIDC lifetime settings; refresh lifetimes stayed unchanged.
2. Submit synthetic update 7601 through normal intake. Observe one real user,
   inbox entry, task, Loom run, version 1 session and pending model step/request.
3. Stop only the owned IAM container. After stopping, observe the unchanged
   original session still valid at 11:55:51.506284Z, expiring at 11:57:08Z.
   The bot, Loom, provider and other five services keep their process epochs.
4. Observe actual expiry at 11:57:11.342824Z with that same session receipt,
   original running run/live lease and pending model step. The bot waits in
   `awaiting_authorization`. No outbox, Telegram attempt or business write exists.
5. Start the same IAM container. Without restarting the bot or Loom or manually
   resuming anything, observe the same session at version 2 by 11:57:21.828121Z,
   expiring at 11:58:46Z. The original task/run and logical model request remain.
6. Supply one manual response for that request. The original model step, run
   and task complete; exactly one outbox row and one mock Telegram attempt/message
   are delivered. No business write or sponsored provider attempt is created.
7. Restore all four original OIDC settings with read-back before retirement.

The mailbox deduplicates HTTP retries by request ID. These checks prove the same
durable model step and logical request, not one uninterrupted HTTP connection or
exactly one inference attempt. Manual inference does not prove unattended model
behavior. This gate covers IAM outage with Loom continuing to run; separate
combined-restart gates retain their own acceptance boundaries.

## Preservation and cleanup

Across the failed attempt, product checks and accepted candidate gate, all 112
primary fingerprints matched except River leader/queue bookkeeping. All 46 River
job rows were identical. Primary bot/poller IDs and process epochs and 132 Loom
runs were unchanged. At this gate, the primary bot remained on
`sha256:1f9d123de594fd40dc27e18f56576dd0ac91046077074de130e85464dce9561a`.

Both outage projects have zero containers, volumes or networks. The temporary
PostgreSQL test container was removed. Candidate/base images remain locally for
review and possible promotion; they consume disk, not running-container memory.
Private evidence stays ignored in `.cache/shuttle-work/running-session-outage-v1-qa`,
`running-session-outage-v2-qa` and `session-fallback-fix-v1`. The failed attempt is
not relabelled as recovered or accepted.

Accepted application archive: 298533 bytes, SHA-256
`116d555744aa8deb2782bf5c54799f5ae0da3d0bc889a48aab38d8ba7d96f93c`.
Accepted Loom archive: 90248 bytes, SHA-256
`74ec2ac0491ba446df9dbfc27dc4fc563953b9c5ca40c110cffea8dda49cb000`.

The later owned primary installation has its own preservation/binary-rollback
proof linked above. Revocation during cached credentials, uncertain
reauthorization outcomes, abrupt Loom kill and production acceptance remain
separate work. Browser CA
trust and the joint zitadeltg check still need the previously agreed user session.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
