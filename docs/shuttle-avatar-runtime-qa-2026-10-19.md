# Avatar shared-form/runtime QA — 2026-10-19

Four shared saved-draft paths now have owned runtime proof on Linux/amd64
candidate schema57 / manifest mvp65. Evidence spans avatar-runtime-r1 and a
corrected form-to-agent rerun on avatar-runtime-r2. They used real Zitadel,
PostgreSQL, River and Loom, the shipped Mini App in headless Edge, mock Telegram
and a Codex-fed deterministic provider. No Loom developer stand, real Telegram
account, production service or installed primary was changed. This proves the
runtime/tool contract, not an unattended model or Telegram WebView.

## Accepted paths

- Native only, first stand: upload recovery, crop, save, authoritative preview,
  explicit generation, authenticated JPEG download and document delivery with
  Loom stopped. Tasks and Loom runs both stayed zero. Download bytes match the
  immutable receipt.
- Agent only, first stand: a synthetic Telegram document arrives; Loom tools
  import its file ID, save the shared crop, finalize it and read delivered status.
- Agent to form, first stand: Loom imports/saves a draft. A normal bot-only SIGTERM
  takes0.657s, exits0 without OOM and preserves its original task/run. The form
  saves version3, previews, generates and downloads. The same Loom run reads its
  current draft and immutable receipt/delivery status, then completes.
- Form to agent, corrected stand: an unrelated version2 draft is seeded before
  opening the form. A new original is uploaded/saved/previewed. The observer
  matches the actual POST save response, rather than an earlier GET. Uploaded ID,
  save path/receipt, agent read and finalization all reference the same new draft,
  distinct from the seeded one. The canonical native operation receipt also
  matches. One task/run completes, with one artifact and outgoing document.

These four accepted cases have four distinct artifact/delivery IDs. The first
stand's native-only and agent-to-form browser downloads match their JPEG hashes;
foreign-owner status reads were denied. Browser processes are closed and EN
runtime screenshots inspected. Earlier RU fixture proof remains separate.

## Rejected proof and preserved failures

The initial four-path verifier counted four drafts, four artifacts/deliveries,
three completed tasks/runs and four outgoing documents. Independent review then
found its form-to-agent observer had selected a preceding GET of the old agent-only
draft, instead of the new native POST save receipt. The actual form saved the new
draft correctly, but the deterministic provider was fed the wrong old ID and
created an extra synthetic document for that older draft. Those aggregate counts
are historical command output, not accepted four-path proof. That handoff is
explicitly invalidated in its gate and repeated on the separate corrected stand.
The earlier three paths remain individually supported. The corrected stand has
two drafts (including its unused seed), one artifact/delivery, one task/run and one
outgoing document. Do not combine both stands into a claim of only four physical
deliveries; their retained transcripts contain five.

The first native upload received400 because the mock HTTPS gateway dropped
X-Zinusya-Operation-Id. Database inspection proved no draft/artifact/task was
created. A localhost test reproduced header loss before the one-header allowlist
fix; independent review found no actionable issue. All25 Linux fixture/provider
tests then passed. On Windows24 passed and one Unix-only permission test skipped.

The next browser observer could not read a response body from the CDP inspector
cache, although upload committed. DB inspection and the exact owner-bound upload
receipt restored that original without another draft. The observer now reads
cloned fetch responses inside the browser, without replacing backend responses.
An initial Linux fixture command also lacked its counted-provider source mount
and exhausted a32MiB tmpfs in the large-journal test; a fresh bounded run with the
exact source mount and256MiB tmpfs passed. Failed logs remain separate.

## Custody and remaining work

The exact release Dockerfile was built with a dedicated2GiB/2CPU BuildKit instance,
which was removed. Both databases and all logs from each stand were archived
before its nine-service project, network and volumes were removed. Temporary
Linux test containers were also removed, without OOM. The original primary remains
schema49 / mvp56 and its observed data/runtime are preserved. The new candidate
image is retained locally; no commit, publication or primary/production install
is claimed.

[Real IAM/Loom administrative retry/dismiss](shuttle-avatar-repair-runtime-qa-2026-10-19.md)
now has separate runtime proof. Actual Telegram WebView, unattended model behavior,
native operator repair UI and production retention/discard policy remain open. The full avatar feature and deployment are not declared finished.
Evidence is retained in `.cache/shuttle-work/avatar-runtime-v1-qa/`,
avatar-runtime-v2-qa, avatar-images-v1-qa and avatar-send-ui-v1-qa. The v1 runtime
acceptance is explicitly invalidated for the form-to-agent identity mismatch;
v2 supplies the accepted replacement and combined gate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
