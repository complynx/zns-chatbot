# Native registration prerequisites: owned runtime QA, 2026-10-16

Accepted scope: actual Telegram command and inline role/return buttons, real
Zitadel-authenticated form APIs, and the same shared event expectation/registration
store, with Loom stopped throughout the user workflow. This is not trusted WebView
or unattended model acceptance. Registration/pass four-path acceptance remains open.

## Image-only activation

The full unmodified release Dockerfile built linux/amd64 image
`sha256:ebd1bdfaaa4d73d0d6d55c1a202f35fa5c0a78a79f1dfafeeb5597156cd0b14c`
from712 release inputs matching the812-input behavioral gate. The dedicated builder
used2GiB, no additional swap, two CPUs and one concurrent build step. It and its
cache were removed; the candidate and previous images were retained.

The original Compose hash was checked against the running container before any
runtime mutation. The candidate model differed only by image. Effective environment,
encryption key, mounts, limits, network/security configuration and schemas37/4 were
preserved. Ingress was stopped and work drained, the bot exited0/noOOM, and a fresh
private PostgreSQL dump was verified before recreation. All75 existing table hashes
were preserved apart from allowed River leader/queue startup bookkeeping; queue
names, creation time, metadata and pause state were compared against the dump.
No migration Down, database restore or key replacement was performed.

Eight companions, including Loom, retained their exact fingerprints during this
activation. A real delegated Zitadel owner token read the profile and current
redacted details. That receipt binds the activation ID, candidate fingerprint,
key hash, owner/subject and timestamp. The original poller container was restored
and completed a fresh polling cycle. Unknown Compose outcomes can be reconciled
by observing the existing candidate instead of forcing another replacement.
Fresh independent operational review was clean after adding the pre-mutation
Compose/live hash check.

## Native prerequisite workflow

Only the authorized current Telegram account and @complynx_test_bot chat were
used. A synthetic event requires legal name and passport number, with Europe/Minsk
timezone and an open sales window. Preparation is explicit fixture work: both
existing encrypted values had to decrypt in memory with the `SYNTHETIC` prefix.
The original encrypted row was backed up privately; an exact-old-state conditional
update installed a valid empty encrypted fixture at version4, after version3.
No old receipt or journal row was deleted. This is not a product clear-data API.

The owned Loom container was stopped with exit0/noOOM. With it stopped:

1. `/register <fixture-event-id>` delivered event and role choices.
2. Clicking Leader produced a pending event-specific details expectation, named
   the missing fields, showed an event-local deadline, and delivered the correct
   Mini App launch and role-return buttons. No registration existed.
3. Locally HMAC-signed Mini App proof authenticated through real Zitadel. Form API
   writes saved synthetic fields at versions5 and6; exact request repeats returned
   the same results. The same expectation completed. Registration was still absent.
4. Clicking Return to role selection and then Leader explicitly created one solo
   waitlist registration. It created no pass or payment.

The four Telegram tasks completed through the structured route without run IDs.
Loom run/input counts did not change: zero agent turns. The final saved-registration message was
visibly present in Telegram. The form completion itself did not cause a join.

Existing events/rules/deposit policies, profiles, passes, pairs, payment evidence,
payment reviews, settings and registrations outside the fixture event retained
baseline hashes. The same bot and poller fingerprints remained unchanged during
QA. Loom was restarted using its original container ID and became ready; the seven
other companions retained their exact fingerprints. Logout succeeded and subsequent
protected details reads returned401. Ten main containers are running, with no
new QA/build containers or volumes retained. Synthetic event, waitlist registration,
protected fields, request history and encrypted backup remain as test evidence.

## Evidence and limits

Ignored checkpoints:

- `.cache/shuttle-work/native-details-images-qa/checkpoint.json` — image provenance
  and builder removal.
- `.cache/shuttle-live-test/native-details-main-cutover/checkpoint.json` and
  `owner-probe.json` — activation/data/security preservation and real IAM reads.
- `.cache/shuttle-live-test/native-details-entry-runtime-qa/checkpoint.json` —
  native tasks, expectation, version progression, business hashes and Loom restart.

The QA helper and synthetic-only encryption helper passed independent source
review. An explicit abort/recovery path restores the recorded Loom container
before inspecting unknown CAS/event/form outcomes; it does not replay business
writes or replace operation IDs. Finalization can observe an already-started Loom
and a previously completed logout. These recovery branches were source-reviewed;
the successful workflow did not fault-inject them. The separate Linux/race
component gate covered interrupted prompt recovery and passed36 roots/38 events.

The Web App button was verified in the delivered payload and visible UI. It was
not opened through a trusted Telegram WebView: CA installation remains deferred.
The real form API used locally signed test proof, not Telegram-issued initData.
No manual provider was required for this workflow because no agent ran. This does
not accept event-specific agent/mixed paths, pair/admin/payment workflows, or
production rollout. No commit, push or production change was performed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
