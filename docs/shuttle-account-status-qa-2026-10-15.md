# Owner account status acceptance — 2026-10-15

Scope: the [owner registration/pass/payment-status read](shuttle-account-status.md)
in the Mini App account page and typed agent toolbox. This is a read-only slice;
no registration/payment form or durable event selection was added.

## Source verification

Architecture and senior Go consultations agreed before coding. Three fresh
correctness/security/quality reviewers found no actionable defects. A fresh
review after scoped formatting/test corrections was clean. Root's later DST
reproduction found two different instants displayed as the same local clock time;
the UI now includes numeric UTC offsets and seconds. A fresh final UI/correctness
review was also clean. No changes were staged or published.

All 168 enumerated integration tests ran exactly once across 12 fresh bounded
Linux race processes, with exact coverage recorded. Explicit cmd/internal/
migrations packages also passed. The 14 gate containers reported no OOM. Final
Go lint reported zero issues. The subsequent UI-only DST change passed JS syntax,
all 12 Node behavioral scenarios and the embedded API race package. Go business
code was unchanged after the full race gate. Native served JS matches final
reviewed source bytes exactly.

Tests cover waitlist/assigned/unpaid/deposit/full balances, pending proofs versus
confirmed money, ended events, disabled-module omission, current owner/bot/org/
subject, administrator isolation, one-connection operation, keyset pages, restart,
logout and expiry while a DB read is blocked. UI tests cover exact integer money,
event timezone/offsets, DST repetition, text rendering, replacing pages, same-page
retry and clearing account details after session rejection.

The first delayed-read test incorrectly observed handwritten `SELECT *` text;
sqlc emits explicit columns. The corrected test observes the actual ungranted
PostgreSQL relation lock. Initial lint formatting/constant findings and the first
DST reproduction failure remain in local evidence beside later passing results.

## Owned native services

Candidate image:
`sha256:9012a87c07b3c12c043597a57f13b88d0e05fb5160a3a50e03ebfb28e882be08`.
Schema remains 24; manifest advances to `mvp-24`. The idle owned bot alone was
recreated. Existing user/task/conversation/outbox/payment counts and saved user/run
event selections remained unchanged. Earlier Mini App migration backup remains
available; this read-only slice needs no new schema migration.

Real Zitadel and public Mini App API reads passed using a locally signed proof for
the authorized current test account. This is **not Telegram-issued initData**.
The HTTPS Origin was configured, but the fixture calls used the loopback HTTP API;
it does not prove HTTPS/browser trust.

Native acceptance covered:

- Nine owner registrations, eight with enabled assigned-pass details, read in
  bounded two-item pages and compared against exact DB ownership.
- Seven administrator-owned registrations, with exact registration UUID checks
  despite event overlap with the ordinary owner. Administrator rights did not
  expose another person's registration.
- Mini App and agent-tool projection parity; confirmed plus remaining equals the
  frozen price, disabled modules omit money, fully paid passes are not overdue.
- Bot restart preserves the session and unchanged account records; logout makes
  subsequent account reads return 401.
- Real Loom dispatched `modules.zinusya_registrations.list` through the script
  tool and completed the same accepted run. The provider was a manual Codex
  mailbox, not an unattended model evaluation.
- No bot jobs, conversations, messages, money evidence/reviews or event pins changed.

Two native helper failures were corrected without product changes. It first
compared an administrator's default five-item page with all seven stored records;
pagination was correct. The stranded disposable session was consumed before a
fresh fixture retry. It also tried `module_call` for a read; Loom correctly
returned `use_script_for_reads`. The same run continued using script, with no
replacement run or Telegram resend. Neither case is an upstream Loom bug.

## Limits and cleanup

Actual Telegram WebView login, iframe/storage and logged-in visual acceptance
remain deferred until Daniel installs the local CA. The Node DOM fixture proves
client behavior, not a real Telegram browser session. No production change or
readiness claim is made. No real Telegram message was sent in this slice.

Completed test/build containers and the disposable QA PostgreSQL/anonymous volume
were removed. The temporary pre-DST image was already absent when cleanup checked
its exact ID. Nine owned capped stand services remain running. Loom developer
checkout/stands were untouched; no new upstream finding was filed.

Ignored evidence includes the exact 168-test batch plan, lint/race/Node logs,
pre/post DST reproduction, native checkpoint and script-run ID. The checkpoint
stores no IAM/app credentials or raw initData.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
