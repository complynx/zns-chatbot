# Massage specialist preference context QA — 2026-10-18

Bounded reactive preference history passes a 42-root race gate: 39 integration
roots, two observability roots and one manifest root, with 82 passing test
events, zero failures and zero skips. Scoped Linux lint reports zero issues.
This is source/mock acceptance with in-process service restart. Main remains
schema48 / mvp-55; no real Telegram, OS/container restart or installation
acceptance for the complete notification feature is claimed.

A new query projects at most five of the requesting account's original
preference receipts, newest first. The operation creation timestamp must be
strictly before the reactive task creation timestamp. Task ownership, current
account scope, deployment, enabled massage module and a current active private
specialist binding in the original event constrain retrieval. No pass or global
administrator bypass is introduced. Revocation or module disablement withholds
history; regrant makes retained original receipts available again.

The context contains operation/event IDs, original interface, original result
and recorded time. It excludes request hashes, session tokens and client data.
It explicitly describes untrusted historical metadata, not instructions,
consent, a current setting or authorization to repeat an action. It explains the
global account scope and directs the agent to read current preference before a
new explicit-version change. Earlier unresolved operations retain their original
operation ID; an inaccessible status does not prove nonexecution.

Both native and agent writes feed this same receipt history. Changes do not
start Loom proactively. An explicit request includes earlier native history in
a new run. After an agent-side change and in-process service stop/start, the
next explicit message steers the same run with both original receipts. Tests
assert one run, the new steering input and absence of the native session token.
They also cover the five-row bound, ordering, later-operation exclusion, foreign
scope/task, module disablement, binding revocation/regrant and account revocation.
Other existing booking and administrative context scenarios are included in the
gate to check the adjacent context hook.

The first runtime attempt failed because the new store-only test called a
service helper before its fixture initialized Service, causing a nil-pointer
panic. The other new restart/steering root passed. Failed logs and the failure
summary remain in the v1 evidence directory. The fixture now uses the existing
direct-store `bindingHistoryTask` helper. Product behavior was not weakened.
A fresh final review covered that change before the successful v2 run.

Fresh correctness and separate quality/runner reviews found no actionable
issues; the corrected fixture also passed a fresh correctness review. Reviewers
used source reads and textual diffs only. Final fixture lint passes, and the
index remains empty. No commit, publication or production action was performed.

Both attempts used exact owner-labelled temporary PostgreSQL and Go containers
with 512 MiB and 2 GiB limits. Neither attempt was OOM-killed. All exact QA
containers and private database volumes were removed after terminal results;
shared caches remain. The successful gate verified frozen source and ten main
Compose container fingerprints. It does not establish live business-row or
standalone-provider fingerprints. Database cleanup is not a restart acceptance
test.

Ignored accepted evidence: `.cache/shuttle-work/massage-specialist-preferences-context-v2-qa/`
contains exact expected roots, source freeze, race log, final lint and cleaned
checkpoint. The v1 directory preserves failed evidence and initial scoped lint.
The prior [API gate](shuttle-massage-specialist-preferences-api-qa-2026-10-18.md)
and [UI gate](shuttle-massage-specialist-preferences-ui-qa-2026-10-18.md) retain
their separate scopes and limitations.

Next: manifest revision, candidate image and independently reviewed schema48-to49
installation/recovery preparation, followed by owned-main and real Telegram
acceptance. Upcoming-session notifications remain a separate milestone.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
