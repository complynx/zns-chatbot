# Avatar administrative delivery runtime QA — 2026-10-19

The owned avatar-repair-r2 stand passes the existing saved-draft delivery
resolution contract on candidate schema57 / manifest mvp65. It uses real Zitadel,
PostgreSQL, River and Loom, mock Telegram and a Codex-fed deterministic provider.
No product code changed in this stage. The primary remains schema49/mvp56.
Actual Telegram WebView, unattended model and native operator repair UI remain
separate work. No production readiness or deployment is claimed.

## Accepted behavior

Two real worker sends are accepted by mock Telegram, then disconnected before
the bot receives their replies. Both intents become delivery_unknown. Their
physical documents remain in the Telegram journal; no automatic resend occurs.

The administrator has zinusya.admin in both the Loom and API IAM projects, as
required by the [administration contract](shuttle-agent-administration.md).
Dynamic discovery exposes the repair methods for that actor. The ordinary owner
gets an empty dynamic method list and HTTP403 from direct inspection. The static
manifest still contains a dynamic administration placeholder; that is not a grant.

The real Loom retry tool creates one exact proposal. The bot delivers its terms,
delivery identity, reason and possible-duplicate warning to the administrator.
The approval operation/arguments match the canonical stored proposal; delivered
outbox and mock transcript are checked before free-text consent is sent. A normal
bot-only SIGTERM takes0.688s, exits0 without OOM and preserves the same task, run,
proposal and unknown delivery. Other services keep their identities/start epochs.

Explicit natural-language consent approves that pending action. The retry creates
one additional document send for the existing artifact and reaches delivered.
Its recorded Telegram message/chat receipt maps to the actual document. A separate
Loom action dismisses the other unknown intent after presented terms and free-text
consent; dismissal produces no document send. It does not prove prior delivery.

For each operation, prepare/confirm/status/reconcile with a nonadmin token return403
without changing proposals, delivery state or the send journal. Exact administrator
confirmation replay and status return the original immutable result without a
new send. A competing prepared proposal is rejected after the retry. Audit retains
prior state, version, attempt and receipt, recipient, actor, reason and resolution
time; the existing JPEG receipt remains unchanged.

Untruncated Telegram snapshots are checked before/after approval, restart,
permission-denied mutations, resolution and replay. Exactly three sendDocument
attempts and three outgoing documents exist on the accepted stand: two initial
uncertain sends and the one explicitly authorized retry. Recipient and downloaded
mock-file SHA256 match the saved avatar data. There are two immutable avatar
artifacts, two completed audit results and one uncompleted competing proposal.

Physical task count is five: one earlier fixture request without a prepared
action, two accepted action tasks and two durable consent-input tasks. All are
completed and all task outbox entries delivered. There are three Loom runs; the
two consent inputs continue the accepted runs rather than creating new ones.

## Preserved failures and review

Initial fixtures assigned the administrator role only in the API project. Direct
API discovery succeeded, while Loom correctly withheld its permission-gated
module. The first stand later paused at platform_retry_limit while its test
provider was unanswered during investigation; it had zero resolution proposals.
Its two original uncertain sends, databases and logs are retained. On the second
stand, restarting only to clear the pre-grant token cache did not fix the missing
Loom-project role. That cache-only hypothesis was rejected. The failed request
completed without a proposal/effect; adding the documented second project role
and acquiring fresh bot credentials enabled the actual Loom action. No Loom bug
or product fix is claimed from that fixture failure.

Independent QA review identified missing journal assertions, missing proof that
terms reached the administrator, and incomplete direct nonadmin mutation probes.
Those checks were added before accepting the gate. A later QA assertion treated
the snapshot_truncated map as a Boolean; its failure is preserved and the same
pending proposal was recovered without creating a replacement. The final verifier
also initially assumed three tasks, omitting the two consent-input tasks. Actual
DB classification corrected the count without repeating user actions. An old
console summary had the same count error; final-summary.json records the verified
five tasks/three runs. Fresh independent QA review reports no actionable findings.

## Custody and limits

Both databases and all logs from each of the two repair stands were archived
before their exact nine-container projects, networks and volumes were removed.
No OOM occurred; the installed primary data/runtime are preserved. The accepted
image is the unchanged local avatar candidate. There is no publication, commit
or primary/production installation in this stage.

Evidence is `.cache/shuttle-work/avatar-repair-runtime-v2-qa/`: runtime-acceptance.json,
final-summary.json, per-boundary Telegram snapshots, sequence-v4-checkpoint.json,
cleanup-proof.json and the final source gate. Failed first-stand evidence is in
avatar-repair-runtime-v1-qa. Earlier component tests cover other expiry/revocation
and concurrency cases; this runtime gate does not claim to repeat all of them.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
