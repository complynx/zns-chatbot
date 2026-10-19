# Specialist notification generation fences — 2026-10-18

Accepted on a fresh disposable `zinusya-mock-notice-generation` stand using the
current schema49 / `mvp-56` bot image. Two deferred old notifications were blocked
after real native setting/authority changes, while fresh-generation notifications
were delivered. All nine processes kept their IDs, images and start epochs during
the gate. This is running-worker acceptance, without a process restart.

## Scope and fixture

The stand had its own PostgreSQL, IAM organization, CA, fake Telegram token and
mailbox. Synthetic users501 (specialist) and502 (client) opened native sessions
through signed mock initData and real IAM. SQL created only the initial synthetic
event, schedule, assigned client pass and active specialist binding/version1.
Preference rows, notification states and later binding changes were not seeded or
modified through SQL. The specialist had no participant pass. No Loom inference or
real Telegram send was involved.

Each stale-intent case started with a real native booking and one mock429 with
retry_after45. The bot persisted the actual `retryable` intent: one rejected attempt
and zero accepted effects. The native changes finished before that same intent was
due. The original notification row was unchanged immediately after those changes;
its captured versions were never rebased.

## Mute followed by re-enable

The initial absent preference was enabled/version0. Native operations disabled it
at version1 and enabled it again at version2. The original intent still captured
preference0 and binding1. Re-enablement finished35.783 seconds before its retry
deadline.

After the deadline the worker marked that old intent `delivery_blocked`, without
another Telegram attempt or accepted message. At acceptance the preference was
already enabled/version2, the binding was active/version1, the client booking was
still booked and no user was revoked. A native specialist read succeeded. This
rules out a passing result caused only by cancellation or an inactive/muted account.
The native cancellation afterward produced a fresh delivered cancellation notice.

## Revoke followed by regrant

Two tracked temporary administrator grants were created in this stand's own IAM.
A second native booking captured preference2 and binding1, then received the same
safe429. Native administrator prepare/confirm revoked the specialist at version2
and restored the same person at version3. Both changes completed31.617 seconds
before the old retry deadline. The original intent row remained unchanged.

The temporary IAM grants were removed before accepting the result. With the current
binding active/version3 and preference enabled/version2, the old binding1 intent
became blocked with only its original rejected attempt and zero accepted effects.
The booking remained booked until this assertion. The subsequent native cancellation
again delivered a fresh notice using current generations.

## Positive control and final state

A third native booking captured binding3 and preference2. With no fault armed, it
was delivered once: one attempt, one accepted message to mock chat501. This confirms
that current authority and delivery worked after the stale intents were suppressed.
The control booking was then cancelled and its cancellation notice delivered.

All three bookings ended cancelled. The final six specialist intents are two blocked
old booked notices and four delivered fresh notices (three cancellations plus the
positive-control booking). The mock retained four messages and six attempts: two
rejected429 attempts and four accepted sends. Both long/short owner reminders for
all three bookings ended blocked. No Loom run was added.

The original effective preference and binding values were restored, with their
monotonic audit versions retained: enabled/version2 and active/version3. No audit
rows were deleted. Both temporary IAM grants were absent. Administrator native
reads returned403 after grant removal; the non-specialist client's preference read
returned404. Both sessions were logged out and their temporary credential file was
removed. The initial SQL staff binding remains only in the retired fixture archive.

## Review, preservation and cleanup

The driver had two independent initial reviews. Safety review found missing source
and stand revalidation on IAM grant cleanup. Before any fixture effects, a scoped
source/exact-IAM guard fixed this while keeping cleanup independent of a running bot.
A fresh review found no actionable findings. AST parsing and the real behavioral
cases passed; there was no Go product change requiring another Go test run.

The accepted state was retained in a304136-byte custom archive and final mock
transcript. The archive SHA256 was rechecked; this is not a restore acceptance claim.
The original frozen runtime manifest and unchanged process fingerprints are retained.
All nine services stopped without OOM, then this exact project's containers, volumes
and network were removed through the existing ownership-checked stand tool. Generic
fixture configuration/credentials stay ignored; native session credentials were
removed.

A fresh primary `shuttle-live-test` check matched112 raw fingerprints except River
leader/queue bookkeeping. Its bot/poller fingerprints, Loom run count and quiet
state were unchanged. No production or Loom developer stand was changed. The Git
index remains empty; no commit, push or publication occurred.

Ignored evidence under `.cache/shuttle-work/specialist-notice-generation-v1-qa/`
includes `checkpoint.json` (`accepted`), source gate, exact native preparations and
receipts, both captured/blocked intents, current authority reads, IAM grant audit,
positive control, retention/archive hash, original manifest, terminal service
receipts and `cleanup.json`. Failed review findings remain part of the review record;
no failed runtime attempt was relabeled as passing.

The [real Telegram delivery gate](shuttle-massage-specialist-notices-real-qa-2026-10-18.md)
and [actual process recovery gate](shuttle-massage-specialist-restart-qa-2026-10-18.md)
remain separate evidence. This gate does not prove browser Mini App controls,
unattended agent decisions, post-reservation retraction, never-attempted pending work,
broader task/outbox restart, combined service outages or full live-controller rollback.
A committed external-send reservation cannot be retracted by a later mute/revoke.
No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
