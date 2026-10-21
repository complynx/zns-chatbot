# Historical avatar references: runtime QA, 2026-10-21

## Scope

The owned `avatar-references-r1` stand runs schema 68 / manifest `mvp-86`,
with real local Zitadel, PostgreSQL, River and Loom. Telegram and the model
provider are deterministic fixtures. Headless Edge runs the shipped Mini App
against that backend. This is not a real Telegram WebView or production import.
The primary `shuttle-live-test` remains schema 67 / `mvp-83`.

## Verified behavior

The actual offline importer creates an 893-byte archive containing two synthetic
users and two historical Telegram image references. It contains no image bytes.
Authenticated administrator prepare/store/apply APIs accept it. Live user, task,
draft and document counts stay unchanged during apply. Each user discovers only
their own locator; reciprocal metadata reads return 404.

The native-to-agent path selects the historical attachment in the form, creates
and saves a draft without starting an agent task, then continues that exact
saved version in a reactive Loom run. Loom imports the historical file, opens it,
and reads metadata. Forced artifact metadata expiry creates a new physical
artifact through `zinusya_history.avatar_refetch`; owner, archive/record locator,
MIME, size and hash remain correct. The agent finalizes the same draft/version
and mock Telegram receives one document. Both historical deliveries are matched
to their exact recipient and Telegram message receipt; JPEG bytes downloaded
back from the Telegram simulator match the expected result.

The agent-to-native path creates and edits the other owner's historical draft
through real Loom tools. The form selects that exact draft, changes the crop,
previews, finalizes and downloads it. The original Loom run reads the exact
native receipt and delivery state without generating another document.

Both saved transforms and receipt draft/version/source identities are checked.
Generated JPEG bytes match expected outputs from a separate offline renderer
process using the known source and transforms. Both edited results differ from
the unchanged-image control. This checks integration and output consistency;
it does not provide an independent rendering algorithm.

Both historical drafts retain their archive/record pair and current owner, with
no source attachment ID and no retained original bytes. Foreign users and the
fixture administrator cannot read them. Agent/UI results contain neither private
Telegram file ID nor unique ID.

A separate reactive availability check forces a Telegram file-download 404.
Real Loom reports source HTTP 503 while the existing finalized document opens
successfully. A native session receives HTTP 422 with an explicit request to
resend or upload the source. Its completed JPEG still downloads unchanged. All
three one-shot file-loss faults are consumed. These transient failures do not
prove persistent Telegram deletion or infer availability from message deletion.

At this historical-handoff checkpoint, the stand contains three live users,
six drafts, six generated documents,
six delivered document intents and seven completed reactive tasks. Relative to
the accepted pre-history baseline, the two requested handoffs add two drafts and
two documents; the handoffs and availability request add three tasks. There are
no additional generation or delivery effects.

## Evidence and review

Local ignored evidence is under
`.cache/shuttle-work/legacy-avatar-migration-v1/history-runtime-v1/`:
`runtime-acceptance.json`, `checkpoint.json`, archive receipts, browser screenshots,
expected render hashes, `telegram-delivery-readback.json` and downloaded generated
JPEGs. Both browser processes
closed. No new container was needed; the existing owned stand remains useful.

Six fresh textual fixture reviews found four acceptance gaps, now fixed. Final
fixture review has no actionable findings. The Git index remains unchanged.

Two fixture failures remain recorded. The first native loss probe expected the
internal error wording instead of the native public message; its response body
was not retained, so it is not accepted as passing proof. A corrected read-only
probe captures the explicit 422 response. The first final verifier compared
PostgreSQL bytea JSON against a plain hex API hash. Its source and diagnosis are
preserved; explicit SQL hex conversion fixes the verifier. Neither correction
repeats draft creation, generation or document sending.

This accepts synthetic historical-reference handoffs, artifact expiry and
source-loss handling. Handle TTL expiry, physical cache-file deletion, real data
export/import, real Telegram/Zitadeltg acceptance and production readiness remain
outside this result. At that checkpoint, legacy generated-avatar runtime refresh
and the reference slice's race gate remained open; see later acceptance below.

## Later preview and race acceptance

The later single-step generated-avatar refresh gate passed on this same stand,
including one exact mock-Telegram document and refresh after artifact metadata
expiry. The source lifecycle document records that separate result.

The historical-photo preview image is now installed only on
`avatar-references-r1`: schema 68 / mvp-86, image
`sha256:68d41b96399ab6fa27616c39cb92542e68fb8f7fe3b63896f3345ef323fcf628`.
Only the bot was recreated; the other eight container fingerprints and four
domain-table snapshots are unchanged.

A fresh headless Edge session uses signed mock Telegram initData and real IAM.
It previews the owned historical photo before any draft write, verifies the
expected 582,591-byte JPEG digest and decoded width within 320px, and receives
404 for a foreign source. One explicit Use creates exactly one version-1 draft
with the expected owner, archive/record and source digest, without original bytes.
The stand now has three users, seven drafts, seven documents and eight completed
tasks. Existing generated JPEGs/receipts, draft delivery intents, outbox rows and
Loom runs are unchanged by this preview/use check. The own Loom container is
paused during the check and restored afterwards; the browser PID is confirmed
absent. Both captured screenshots were visually checked.

The full immutable-source Linux race run completes all 833 discovered integration
roots in 42 groups. Across all tested packages, 1,148 roots pass and one watchdog
helper is intentionally skipped, with no failed root, unexpected skip, data-race
warning or OOM. Its observer times out at 3,300 seconds, but the same container
continues and exits 0. A separately reviewed completion step archives/decodes the
database and removes only that container and database, without replaying tests.
The original timeout proof remains unchanged. Two later deadline files have
separate 15-case behavior and strict-lint checks; the full race result belongs to
the frozen snapshot and does not cover those later files.

Ignored evidence is under `legacy-avatar-migration-v1/history-preview-install-v1/`,
`history-preview-runtime-v1/` and `full-race-v2/`. The latter contains the preserved
original proof, separate terminal completion report and frozen-source assessment.
The primary preservation check passes. This extends synthetic stand acceptance;
real Telegram WebView, actual exports, joint ZitadelTG checks and production
readiness remain open.

## Primary snapshot migration rehearsal

A logical snapshot of the owned primary at schema 67 is restored into a new
dedicated database on the secondary PostgreSQL. It contains seven users, two
generated JPEGs and no avatar drafts. The exact installed candidate runs only
`-migrate`, twice, without application workers or Telegram/Loom/IAM calls. Schema
68 is reached and all 126 public data tables match their pre-migration row counts
and content fingerprints; Goose version metadata is excluded from that comparison.

The Down SQL runs successfully inside an explicitly rolled-back transaction.
The clone remains schema 68 with unchanged data. This is not a Goose downgrade
or old-binary rollback test. Because this snapshot has no drafts, it does not prove
conversion of populated Telegram drafts; the separate behavioral migration test
covers Telegram references, retained browser uploads, old operation replay and
refusal of a lossy rollback.

Both source and migrated-clone dumps are fully decoded. The migration container
and clone database are removed; neither live stand is migrated or recreated.
Ignored evidence is in `legacy-avatar-migration-v1/primary-migration-clone-v1/`.
The original primary remains schema 67 / mvp-83. Actual exports, primary update
and production acceptance remain separate gates.

## Archive-validation isolation and proxy fidelity

The later candidate from commit `0599dfef` is installed only on the owned
`avatar-references-r1`, at schema 68 / mvp-86, image
`sha256:5f912a3dd7476f7270bde921bd372ff217a41e0ee521d243f87c4c1ace7478ac`.
Only its bot is recreated; the other eight service fingerprints and four captured
domain tables match. Exact-commit remote CI is still running at this installation;
this permits isolated candidate QA, not primary promotion or full acceptance.
The primary remains schema 67 / mvp-83 and passes its preservation observer.

A bounded incomplete archive-validation upload occupies the shared admission
slot. Uploaded and Telegram originals both remain HTTP 200 with unchanged bytes,
size and MIME. The historical-photo preview returns 503 while occupied. Finishing
the deliberately invalid upload returns 400 and releases the slot; the historical
preview then returns its identical 582,591-byte JPEG. Six domain-table snapshots
and Loom run IDs/states remain unchanged. The four fresh historical create/finalize
commit-backpressure cases and completed-receipt replays are separate focused
PostgreSQL/race evidence; this live read probe does not claim render-barrier write
coverage or real Telegram WebView acceptance.

This probe also exposes a mock-proxy defect: the test HTTPS relay drops the bot's
privacy/security headers. The bot itself sets them. The narrow relay allowlist now
preserves `Cache-Control`, `Content-Disposition`, `Content-Security-Policy`,
`X-Content-Type-Options` and `Referrer-Policy`. A behavioral reproduction fails for
both successful and denied private-file responses before the fix. The nearest
Linux HTTP/process suite then passes all 20 tests without skips or OOM; its bounded
256 MiB container is removed. The Windows run passes the HTTP cases and retains
one expected POSIX-only SIGTERM skip. Independent source review is clean.

Only the secondary mock Telegram container is stopped and started, retaining its
ID/image and stored state. The stop command plus post-stop inspection takes 1.266
seconds and reports exit 0 without OOM; this is not a measurement of the bot's
one-second graceful-drain requirement. Three successful file responses and one
unauthenticated 401 are compared directly against the bot and through HTTPS.
Status, body digest and the selected headers match exactly. All three successful
responses retain `private, no-store`; the 401 retains `no-store`.

The first final observer incorrectly expects an unchanged mock read-attempt
journal. A read-only follow-up also reaches the existing 200-event page boundary.
Both failures are retained. A complete two-page read reconciles all 208 events:
the original 200-event prefix is intact and exactly four successful `getFile`
attempts add eight normal attempt/result events. Prior attempt rows, messages,
updates, holds, domain rows and all other service fingerprints match. Neither
restart nor the four HTTP comparison cases is replayed for reconciliation.

Ignored evidence is under `legacy-avatar-migration-v1/archive-budget-image-v1/`,
`archive-budget-install-v1/`, `archive-budget-runtime-v1/`,
`mock-private-headers-v1/` and `mock-private-headers-runtime-v1/`. The original
assertion snapshot remains separate from the reconciled acceptance report.
No production deployment, actual legacy export or joint Telegram/ZitadelTG
browser-login acceptance is established by these checks.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
