# Native avatar editor source QA — 2026-10-19

This record covers the visual draft editor at source schema55 / mvp63. The
installed primary remains schema49 / mvp56. No build/install, deployment,
publication or real Telegram action was performed during this stage.

## Executed proof

The CI mini-app Node command passed200 tests, including the first ten avatar
editor behavioral tests. After final RU list-label changes, the focused avatar
command again passed all ten. These cover exact transform units and pointer pan,
identity reset, saved-version preview, continuing agent edits, stale-write field
preservation, request persistence before writes, lost save/upload replies,
reload recovery, exact-file reselection, unavailable/corrupt storage, owner
changes, session expiry, stale responses and stale terminal errors. The public
asset test now includes avatars.js and the restricted self/blob image CSP.
The API unit and lint outcomes are retained separately in the source gate.

An owned headless Edge fixture used the real HTML section, CSS, controller,
DOM/canvas and image decoding, a128x64 generated PNG and the actual pinned frame.
Its mock HTTP backend enforced a fixture authorization header and CAS/receipts.
It checked responsive390px layout and pointer dragging, exact saved values,
JPEG display under CSP, agent-state continuation, stale native save/reload,
lost reply recovery across browser reload without another save, raw original
upload and Russian labels. The final browser/HTTP server closed successfully.
The inspected EN/RU screenshots are retained privately.

This is browser acceptance against a mock avatar backend, not the installed
runtime, real IAM, a real Loom conversation or Telegram. The preview image
response in this browser fixture uses the previously verified CLI JPEG; the
fixture does not independently rerender its new original. The previous
[preview API QA](shuttle-avatar-preview-qa-2026-10-19.md) supplies separate
PostgreSQL/provenance proof; this UI command does not rerun those tests.

## Review and retained corrections

Two independent initial reviewers found a failed-switch selector mismatch and
a stale terminal-error path that could clear a replacement owner's pending
record. Source checks confirmed both; the controller now restores selection on
failure, commits replacement options after successful loading, and epoch-fences
rejections as well as fulfilled reads. Two focused critical-path tests passed.
A fresh textual-diff review found no remaining actionable issue. A further fresh
review covers final localization and the expanded asset test; results are in
the source gate. Reviewers executed no commands or tests.

The first browser fixture waited for a reused success message after upload,
which could capture the prior state while the upload was still running. That
attempt remains retained and is not final upload-completion proof. The final
fixture waits for no pending operation, enabled reload and identity fields,
then asserts the draft ID changed. Final screenshots also verify the Russian
version label. Initial and final evidence are separate.

Ignored evidence is `.cache/shuttle-work/avatar-editor-v1-qa/`: Node logs,
Go API JSONL, lint log, browser-v1/v2 checkpoints and screenshots, review records
and the final source gate. No Docker container is required by this UI fixture.
The source gate records primary preservation and the unchanged empty index.

Finalization, task-free durable send, full four-path runtime acceptance and
production readiness remain unproven. See [editor contract](shuttle-avatar-editor.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
