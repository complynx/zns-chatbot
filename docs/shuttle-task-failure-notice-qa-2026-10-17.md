# Terminal task failure notice QA, 2026-10-17

Source component, owned image-only rollout and an actual English Telegram
failure notice are accepted within the scope below.
No migration, API contract, authorization or retry policy changed.

## Observed problem and change

Real massage QA committed a booking, then Loom ended with `active_time_expired`
before the final answer. Telegram displayed only a generic task failure. That
message did not explain that the booking could remain or tell the user to check
before retrying. The original failed run is retained in the
[massage runtime report](shuttle-massage-runtime-qa-2026-10-17.md).

The generic failed-task notice now says actions may already have completed and
asks the user to check their status before retrying. Both English and Russian
texts are updated. It does not claim that a particular action completed, failed
or rolled back. Denial, cancellation, queue state and effect handling are unchanged.

## Behavioral evidence

A real isolated PostgreSQL test commits a massage booking while a mock Loom run
is awaiting input, then makes that peer report `failed/active_time_expired`.
The mock Telegram peer receives the localized warning. After a bot-service
restart, replaying the same update returns the same failed task; there is still
one run, two messages including the original question, one booking operation and
the original booked record and receipt. The test covers English and Russian
saved interface preferences. This is component QA with mock upstream peers,
not a fresh real-Loom or Telegram acceptance of the changed message.

The unchanged text failed both locale assertions in the first run. That expected
failure and its original source snapshot were retained. The fixed candidate
passed four Linux race roots, six pass events, zero failures and zero skips.
The first lint run found one formatting issue; after formatting the two scoped
files, the final same four-root race run and lint passed with zero issues.
The final source snapshot remained unchanged during that run.

Correctness, skeptical quality/usability and a fresh final independent review
found no actionable findings. The Git index remained empty.

Every disposable PostgreSQL/test container and its owned volume was removed
using checked IDs and ownership labels. During component QA, the main ten
Compose services retained their fingerprints. Runtime installation followed
separately as described below.

## Owned rollout and actual Telegram notice

The unmodified release Dockerfile built the exact accepted 825 release inputs as
`sha256:bfda37f3897bffbb7aba010185a21a8a7f4282d5fa6db0d96563556b23a7dab0`.
The dedicated builder had a 2GiB memory/no-swap limit and was removed with its
cache after successful completion. No schema migration was needed: schema46 and
manifest `mvp-49` remain unchanged.

The owned bot was updated using an image-only Compose override. Configuration,
credentials, mounts and security/resource settings were preserved. After stopping
the ingress poller and draining tasks, the bot stopped in 0.75 seconds with exit0
and no OOM. A private database backup was taken. All compared business table hashes were
identical across startup, and the eight companion containers retained their
identities, images and starts. The same poller was resumed. No production or
Loom developer stand was changed.

A new message in the authorized Telegram account/chat requested a read-only
massage-status check. For this synthetic run only, the test conditionally raised
the persisted active-time counter beyond its existing 600-second snapshot limit.
It did not write a terminal state. Actual Loom lifecycle processing subsequently
reported `failed/active_time_expired`; Shuttle delivered the new English notice,
confirmed both in Telegram Web and its delivered outbox record. The saved bot
interface language was English; Russian delivery is covered by component QA.

The existing booking from a previous completed run retained the same row hash,
state, version and single operation receipt. No new business effect was performed
inside this failed run. Thus this is real transport/lifecycle acceptance with a
fault-injected time counter, not a natural ten-minute timeout, an unattended-model
run or a new real after-effect business scenario. The original after-effect
failure remains historical evidence; the new after-effect preservation test uses
mock peers and real PostgreSQL as stated above.

An initial read-only settings probe returned403; no setting or grant was changed.
The first counter-injection guard also stopped before mutation because it assumed
both applications shared internal owner IDs. The corrected probe joined Loom's
owner to the same verified Zitadel subject as the Shuttle task before a single
conditional counter update. These harness observations remain in the checkpoint.

## Remaining acceptance

Lost responses, uncertain applies, readable approvals, specialist lifecycle and
broader roadmap acceptance remain separate work. Real Russian failure delivery
and a natural elapsed-time reproduction were not run. No production readiness
is claimed. Private backups and credentials must not be published.

Ignored local evidence: `.cache/shuttle-work/failure-notice-v1-qa/` (expected red),
`failure-notice-v2-qa/` (first green and formatting finding), and
`failure-notice-v3-qa/` (final green, freeze and cleanup). Image evidence is in
`.cache/shuttle-work/failure-notice-images-v1-qa/`; rollout, backup and controlled
Telegram fault evidence are in `.cache/shuttle-live-test/failure-notice-main-v1/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
