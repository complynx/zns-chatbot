# Native pass-payment primary acceptance, 2026-10-20

The owned local primary is schema64 / `zinusya/mvp-74`. Eleven services and the
original 134 Loom runs are preserved. Production and the Loom developer's stand
were not changed.

Installed image:
`sha256:10f4d80a8a0aaf35bb02e8750f95011586885051765ce202c69d3d1f44ec3df2`.
Verified Linux binary:
`d9303186bf42a92cd6d8ed0c1e2a7effecb2b0d4c6c32ccd3dadd14a1c861c4e`.

## Installation and rollback

The first promotion expands schema63 to64, adding only the native payment action
history table and index. Existing rows, sequences, functions and triggers are
preserved apart from the recorded migration/schema metadata. The new table is
empty in the primary. Candidate/old/candidate binaries (`mvp-73/72/73`) run against
the expanded schema64; no Down migration or old Goose Up is executed. Its three
normal bot stops take 0.781, 0.781 and 0.750 seconds. The migrator exits with code0
and is removed.

The later authentication fix is binary-only: `mvp-74/73/74` runs on the unchanged
schema64 with original data and companion containers preserved. Bot stops take
0.719, 0.828 and 0.734 seconds. Each stop exits with code0 and no OOM. Ingress is
quiesced before each promotion and is running again at completion.

Each promotion retains three custom archives (Shuttle, Loom and Zitadel), fully
decoded with pg_restore. Restore into new databases was not exercised. Schema
rollback is not part of binary rollback; migration64 refuses to drop nonempty
native history without an explicit export decision.

## Failure found by installed HTTP acceptance

The first mvp73 installation passes migration and binary rollback but fails the
unauthenticated payment-target read: it returns503 rather than401. This failed
read-back is retained in `native-pass-payment-install-v1/http-readback-failure.json`.
It was not treated as a full installed HTTP PASS.

`miniAppBearer` returns `identity.ErrDenied` for missing or malformed credentials.
The shared payment error mapper previously classified that as temporary service
failure. The follow-up routes that exact error through the existing401/session-
reopen response. It does not broaden event, owner or reviewer permissions.

The behavior test covers six real HTTP routes and three invalid authorization
forms, with untrusted cookies and unusable store/IAM clients. The valid-route
old-mapper run has twelve failures and six passing controls; all eighteen pass
after the fix. The complete API unit package passes20 roots, and20 PostgreSQL
payment/review scenarios pass without skips. Lint and the Linux build pass.
Initial test-URL and lint failures remain in the ignored evidence. Fresh normal
and quality source reviews have no unresolved findings.

The installed mvp74 read-back verifies all17 current Mini App assets byte for
byte, no-store headers, and401 for payment targets without a session. Custody and
full data read-back pass afterward. No business action, Telegram message, IAM
grant or new Loom run is created by this installation check.

## Evidence boundaries

The [four-path financial runtime gate](shuttle-native-pass-payment-runtime-qa-2026-10-19.md)
was accepted with mvp73: eight confirmations, both same-participant interface
handoffs, immutable original receipts and lost-response recovery. The complete
old source tree is reconstructed before the auth build; only the shared error
mapper, manifest revision and new HTTP test differ. The mvp74 follow-up uses the
bounded API/PostgreSQL checks and installed HTTP acceptance. The full four-path
stand was not rerun with mvp74.

The promotion controller also receives a reviewed fail-closed correction: when
resuming a pending ingress start, failed custody verification fences ingress once
the recorded CLI is known to have settled. A local simulated before/after proof
covers that branch and verifies no fencing effect while the CLI remains live.
This is simulation evidence, not a live fault-injection run.

Ignored evidence is retained under `.cache/shuttle-work/` in
`native-pass-payment-install-v1/`, `native-pass-payment-auth-v1/`,
`native-pass-payment-auth-image-v1/` and `native-pass-payment-auth-install-v1/`.
Current custody uses `installed_primary_observer_v21.py`. All temporary payment
runtime stands, PG fixtures, image probes and the migrator are removed. Only the
eleven primary services remain running. The Git index remains empty; this slice
is unpushed.

Whole registration-through-paid-pass, complete pair joining/assignment, real
Telegram WebView, unattended model comprehension, a new full-repository/race run
and production deployment remain separate gates. Continue the
[registration/pass matrix](shuttle-registration-pass-parity-plan.md), then orders,
meals, massage and the deferred joint linking/WebView checks.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
