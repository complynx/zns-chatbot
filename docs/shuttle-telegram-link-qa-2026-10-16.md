# Explicit Telegram IAM link adapter QA, 2026-10-16

## Accepted behavior

`internal/identity/links.go` adds a server-side IAM operation for the trusted
[prelink design](shuttle-telegram-prelink-plan.md). It requires the exact active
verified human account in the configured organization and its recomputed numeric
Telegram synthetic address. It composes `telegram:BOT_ID:OPAQUE_SUBJECT` using the
actual opaque provider subject, separately from numeric Telegram ID. No route or
agent tool exposes the operation yet; proof verification and local receipt state
remain the caller's future responsibilities.

The operation lists the account's links and rejects a different subject for the
configured IdP. It accepts only complete listings of at most100 links. After a
write, lost response or409 it requires exact read-back on the same IAM account,
then checks that the account is still active. A403 remains denied. Malformed,
empty or oversized successful replies are classified unavailable rather than
proof of rejection; this also corrects the shared IAM request helper. Semantic
trust mismatches and explicit HTTP authorization failures remain denied.

## Behavioral and Linux checks

The IAM wire fixture covers exact reuse, preserved opaque subject, concurrent
calls, lost write reply, foreign-owner conflict, changed subject, stale/partial
listing, absent metadata, account deactivation, wrong caller scope and committed
writes followed by empty/malformed/oversized success responses. Empty protobuf
JSON listings may omit zero count/result; nonempty partial responses are rejected.

The committed-write/malformed-success cases first failed with the previous error
classification and passed after correction. A real-IAM omitted-zero response also
produced a failing behavioral test before the wire-compatibility correction.
Independent source review initially found the malformed-success gap; fresh final
review returned No actionable findings. Strict scoped lint reports0 issues.

The shared request change passed package checks for `cmd/...`, `internal/...` and
`migrations` on Linux. PostgreSQL-dependent tests were skipped without a test DSN;
this is not a new complete database/integration-suite acceptance. Final IAM
package tests and Linux race detection, including concurrency and omitted-zero
coverage, passed. An earlier Windows broad run failed two avatar subprocess tests
because `sleep` and `head` were unavailable; that failure is retained separately.
All temporary Go check containers were memory/CPU/PID bounded and auto-removed.

## Real owned IAM

A uniquely named JWT-provider fixture was created only in our disposable IAM
organization, with auto-linking, creation and profile updates disabled and no
login-policy attachment. Two nonprivileged synthetic fixture humans used stable
UUIDs recorded before creation; neither was a Telegram chat participant or local
bot account. Their opaque provider subjects were deliberately unrelated to their
numeric Telegram IDs. Existing user privileges and login policies were unchanged.

The native probe used the same mounted YAML plus container `SHUTTLE_*` startup
overrides as the running bot, through the application's config loader. Ambient
host overrides were excluded. TLS verified the existing public local CA with
probe-specific loopback DNS; no browser trust or real Telegram proof was involved.

The first fixture setup stopped before IAM changes because the runner looked for
YAML-only values in env. The corrected setup preserved the same fixture IDs.
The first Go probe then stopped before a link write because real Zitadel omitted
zero count/result fields. Its authoritative empty link read and failure remain
recorded. The wire-compatible retry reused the same IdP/users and passed:

- IAM committed A's link, and the probe deliberately discarded its successful
  response. The adapter reconciled the exact link from IAM.
- Repeating A's operation issued no additional link write.
- Linking B to A's external subject and changing A's subject were rejected.
- Fresh final reads proved A retained exactly its original binding and B had no
  links. There were two total POST link attempts: A's accepted write and B's
  rejected foreign-owner attempt.

Ten business-table hashes and task/inbox/outbox/local-user counts matched before
and after. App, IAM, Loom, PostgreSQL and gateway container IDs/start times were
unchanged and non-OOM. No Telegram message, business operation, migration,
production deployment, commit or push was performed. Main bot image remains the
previous accepted image; this source helper is not wired into that runtime yet.

## Evidence and remaining work

Ignored `.cache/shuttle-live-test/iam-link-qa/` retains the setup checkpoint with
historical failures, provider fixture request/ID, stable user IDs, first empty
link reads, final probe result and source hashes. `checkpoint.json` is passed with
stage `native adapter accepted`. Credentials exist only in ignored private inputs
and must not be published. The two IAM fixtures and non-login-enabled provider
remain for follow-up backend checks; no extra runtime container remains.

Next implement durable prelink receipts, uniqueness protection for local IAM
bindings, trusted assertion verification and the isolated provider hook. Then
accept provider-first/bot-first/Mini-App-first convergence, local revocation,
restart and lost completion behavior. This adapter QA does not complete those
flows or interactive Telegram OIDC acceptance.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
