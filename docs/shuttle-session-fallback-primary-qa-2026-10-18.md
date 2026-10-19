# Owned primary installation and binary rollback

Accepted on 2026-10-18 on `shuttle-live-test`. The primary test bot now runs
`sha256:46a9abfd65e3f7d9138f7e106a640997551fa5f191bbff428c3b1486c32c03eb`,
the Linux candidate accepted by the preceding
[session-expiry/IAM outage gate](shuttle-session-fallback-outage-qa-2026-10-18.md).
This is an owned test-stand installation, not production deployment.

## Scope and actual transitions

Only the bot image changed. The resolved Compose model was compared with the
previous model, permitting exactly that image substitution; both full model
hashes were pinned and checked before each recreation. Environment, keys, bind
mounts, ports, user, entrypoint, command, security/resource settings and networks
were retained. There was no schema migration, dependency change, IAM grant or
Loom developer-stand operation. No test Telegram message or model answer was
submitted.

The controller first stopped the original poller, drained already accepted work,
and stopped the idle bot. It retained the original database archive, then
performed three actual application starts against that same database:

1. Start the new candidate and verify readiness, `zinusya` / `mvp-56`, and data.
2. Stop it, recreate the previous image
   `sha256:1f9d123de594fd40dc27e18f56576dd0ac91046077074de130e85464dce9561a`,
   and verify that the old binary still starts and preserves data.
3. Stop that instance and recreate the candidate; repeat readiness/data checks.
4. Start the original poller container and observe a fresh successful polling
   cycle. Reinspect exact bot/poller identities and process epochs before final
   acceptance. No duplicate/replacement poller was created.

The three idle bot stops exited 0 without OOM or kill, in 0.750, 0.688 and 0.812
seconds as measured around Docker CLI stop calls. The poller stop took 0.437
seconds. These are idle-stop measurements, not proof of the one-second drain
budget under loaded or abrupt-kill conditions.

## Preservation evidence

All 112 application table/sequence/schema fingerprints matched, except River
leader bookkeeping and queue `updated_at`. The complete queue rows excluding
only `updated_at` matched: names, creation times, pause state and metadata were
preserved. All 46 full River job rows were identical. The same comparison also
matched the preceding accepted outage gate, so this proof does not silently
reset the baseline at installation.

Application schema 49, Loom schema 4, PostgreSQL cluster
`7696252174528131106`, application database OID 16385, 132 Loom runs, all eight
companion-container identities/process epochs and the independent provider
were unchanged. The bot container was intentionally recreated for each binary
leg; the final container is
`e9c1f2340377ec3117c230ee3a06bd5a4d3dc6fa940f26770393d606bd631bf6`.
Its restart count is 0. The poller retained its original container ID and had
one intentional stop/start. Final readiness and quiet task/outbox checks passed.

The pre-install custom archive is 2838170 bytes, SHA-256
`cdf164026526cc05feb6d916483763a0a7798a459669abef4b4a3d42d31e6799`.
Its header and `pg_restore --list` were checked, including application tasks and
specialist-notification tables. No database restore was executed. The tested
rollback is the actual old-binary start against the unchanged original database;
it is not a claim that an archive restore or prior schema-migration rollback
was accepted.

## Review and operational checks

The separate controller preserves the old accepted schema 49 installer and its
proof unchanged. It pins 535 source/config/evidence paths, including the tested
candidate source manifests and their referenced files. Durable checkpoint writes
precede stop, recreate and start operations. Pending stop/activation/ingress
phases observe the same operation and do not repeat a mutation on a timeout.
Archive recovery adopts only the recorded, hashed complete archive or performs
another read-only export while services remain stopped.

Independent correctness/safety loops fixed overly broad queue allowances, stale
final process observations, missing full-model/source checks, resumable backup
handling and durable intent writes. The final v4 reviewers were clean. An actual
second process was denied the controller lock before any effect; the OS releases
that lock when the holder exits. The successful rollout used nine bounded phase
advances. Final independent runtime inspection repeated identity, schema,
manifest, data, queue and companion checks.

Private plan, checkpoint, archive, final bot/poller logs, lock/review receipts,
source/config hashes and summary remain ignored in
`.cache/shuttle-live-test/session-fallback-primary-v1`.
No disposable QA container was created for this installation; the previous
outage fixtures remain retired. Candidate/old images and backup evidence are
retained for recovery. Nothing was staged, committed, pushed or deployed to
production.

The prior candidate gate's 13 focused tests, scoped lint and real IAM/Loom expiry
acceptance still apply to the exact installed image. This transition did not
repeat those tests or add a real Telegram/model exchange. Browser CA/zitadeltg
joint acceptance, revocation/cache boundaries, uncertain authorization outcomes,
abrupt Loom kill and unattended-model behavior remain separate gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
