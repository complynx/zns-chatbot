# Prelink concurrent-first-login and real-IAM outage checks

The [combined fixture](shuttle-prelink-combined-qa-2026-10-16.md) was extended
using its frozen harness and the same actual candidate bot/provider images. Fresh
PostgreSQL/schema33, TLS gateway, protocol peer, signing volume and two synthetic
identities were isolated under `zinusya-prelink-resilience-20261016`. Telegram proof
and the final relay remained synthetic; the real owned Zitadel was deliberately
stopped and started. No Telegram messages or Loom runs were submitted.

## Concurrent initial logins

Two independent provider login sessions for the same fresh numeric/opaque identity
ran in parallel. A proxy barrier held their first two distinct assertion bodies
together before forwarding, so this was not a sequential repeat of an established
login. Both callbacks returned303. The actual API/database/IAM path produced one
canonical local user, the same IAM subject UUID, one exact external link and two
completed receipts. Both expected final relays occurred. This is one synchronized
pair, not a stress/load test or a concurrent-controller claim.

## Actual owned IAM outage

The scenario pinned the real `shuttle-live-test-zitadel-1` container ID/image and
project ownership, persisted a restore marker, then stopped only that container.
The real bot/provider fixture stayed running. A fresh second identity's callback
returned503. All three provider attempts carried the same assertion body, no final
relay occurred, and the local intended UUID remained saved with no IAM binding.

A `finally` path started the same IAM container and waited for discovery to return
the exact original issuer with verified TLS. The IAM ID/image were preserved; its
start time changed as intended. A new valid provider login then completed against
real IAM with exactly the saved local UUID and one explicit external link. The
prior pending receipt remained an audit fact. This proves same-identity recovery
through a fresh login, not replay of an expired assertion, an automatic browser
retry or recovery of a remote write interrupted after acceptance.

Final fixture state was two users, three completed receipts and one pending
outage receipt, with both bound subjects equal to their local UUIDs. The code does
not infer confirmed IAM effects from pending local intent.

## Review, isolation and disposal

A fresh static review found a Python fixture-isolation defect: the imported
helper's default network argument retained the earlier fixture name. An explicit
wrapper now resolves the new network at call time and preserves deliberate
`container:<peer>` networking. A new independent review found no actionable
findings before execution. The frozen base harness itself was unchanged.

The actual IAM stop and all five fixture cleanup exits were0 with OOMfalse. Both
new real IAM users and the temporary IdP were deleted; subsequent GETs confirmed
absence. Exact labeled containers/network/signing volume were removed. Other nine
main stand container IDs/images/start times matched before/after; the one planned
IAM restart is the explicit exception. The original issuer is healthy again.
Main bot image/config/schema32 were not switched during these checks.

Ignored evidence: `.cache/shuttle-live-test/prelink-resilience-qa/`; wrapper
`.cache/shuttle-live-test/prelink_resilience_qa.py`, SHA256
`be55b4857c2ff9349be8a30e738f4911223236fa2edd187614c6af1d26bd5e07`. The checkpoint records the old/restored IAM identity and an
in-progress marker. After interruption, inspect that exact container and use the
wrapper's `restore_iam` phase before cleanup; never restart a replacement merely
because observation timed out. The wrapper supports prepare/start/exercise/
restore_iam/cleanup and retains any failures. Private checkpoints/env are not
publishable artifacts.

## Remaining acceptance

The native prelink happy/conflict/nonce/revocation/lost-reply/completed-login
restart paths and this synchronized concurrency/outage pair are now covered.
IAM failure during an already accepted write and higher load remain separate
cases. This does not establish real Telegram Web Login, final IAM session-cookie
relay, browser trust, mixed menu/agent registration, or production readiness.
The subsequent [main bot image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md)
now passes with prelink disabled and a fresh backup. [Old-image startup on schema33](shuttle-prelink-images-qa-2026-10-16.md)
has its scoped rollback gate. Main provider/backend activation and real interactive
login remain deferred separately.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
