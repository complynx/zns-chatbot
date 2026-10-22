# PDF file-ID consumer QA, 2026-10-22

Current Zinusya/Loom passes the bounded PDF processing and access checks below.
A fresh independent intake also completes with one delivered reply. The earlier
fixture run did not complete: two unanswered model requests became unknown,
and one explicit resume returned to the retry limit. That failure is preserved;
this is not automatic provider recovery or production readiness.

## Versions and isolation

The nine-service `pdf-font-r1` stand used actual Zitadel, PostgreSQL, published
Loom and its isolated file worker, with mock Telegram and a manual counted
scenario provider. No real Telegram message was sent. No developer checkout,
developer stand or production service changed.

- Zinusya source: `4f03e5ff`, schema 70 / `mvp-91`.
- Zinusya image: `sha256:a9c2d76e5f3a01889ef041e4bfba8131433a84241829e6cbd1656815e00869fd`.
- Loom revision: `34f46468`; image `sha256:003529b117602d52905e1d5f65d353fbe45e969dfc065eae638627b379f813ff`.
- File worker: `sha256:0a80b34f827cc7db5900f5c6662f62fa83dc8c8073dfd69e3f5fb631b31beb83`.

The font-equipped worker was reused. Its earlier successful
[2026-10-15 acceptance](shuttle-loom-upstream-qa-2026-10-15.md) and the original
fontless-worker failure remain separate dated evidence. No rebuild was needed.

## Processing and ownership

The source is a synthetic one-page PDF with unembedded Helvetica and
Helvetica-Bold. It states that no actual payment occurred. The new complete case
uses a distinct synthetic user, mock file, update, task and run:
`57f038ab-603a-4da3-b584-24318c2d9581`.

The incoming document becomes an opaque Zinusya file ID. The initial model
request contains that ID, without the Telegram file ID or source payload.
The controlled agent invokes `modules.artifacts.file` with `zinusya_files.fetch`,
then `pdf_text` and `pdf_render`. This passes through the actual broker and worker;
no file bytes are uploaded directly to Loom at run creation.

The extracted text contains the synthetic banner and reported amount. Authorized
service delegation downloads the derived PNG. It is 800 by 434 pixels, with
13,564 dark pixels. Visual inspection confirms readable heading, scenario,
amount and no-transfer warning. SHA256:
`cf3213386bdd77673e8bcff31c3e0c74c9f9bc0db3ebb87cf71e82e573117b34`.
The fresh run has one completed tool step, two completed model steps, no unknown
model result and exactly one delivered reply for its task.

The first case separately checks another user's source metadata, source bytes
and derived PNG requests: all return 404. A direct user JWT without standalone
Loom permissions returns 403; the successful and cross-user artifact checks use
Loom's documented service-and-subject delegation, as the bot does. No extra
user role grant was added to make the download succeed.

Two mock Telegram download 404 faults produce Zinusya 503 for owner fetch and
owner content download. Fetch returns no artifact descriptor; the content error
contains no PDF payload or Telegram file ID. Once the two faults are consumed,
fetch returns the same descriptor, checksum, MIME and size. This is simulated
source unavailability, not a test of deleting a real Telegram message. It does
not test Loom cache expiry or require an already cached derivative to disappear.

## Preserved unsuccessful case

Original run `025c917a-e6fd-45cd-8329-38ea19f7b657` extracted and rendered the PDF.
During follow-up QA, two unanswered model attempts ended as
`platform_paid_unknown`; the run paused at `platform_retry_limit`. An explicit
resume returned 200/queued, then the same run paused again without a new model
step. No repeated resume, forced billing reconciliation or original tool replay
was used. The run was explicitly cancelled before the distinct fresh case.
Both unknown model rows and all earlier completed step results are unchanged.

Other retained harness failures include setup rewriting a static checkpoint,
a stale pending-model cursor guard, and the initial direct-user artifact 403.
Setup was not replayed. Reviews also corrected outgoing-message counting before
execution; a portrait-page assumption was replaced with the actual page ratio.
These records are not rewritten as successful runs. The complete fresh driver
was reviewed before new inference began, avoiding a pause for code review
between its two model responses.

## Retirement and limits

The first task/run is cancelled; the fresh task/run is completed. The stand has
two source files. All outbox entries are delivered. Payment evidence, payment
reviews and passes remain empty. The provider is controlled; this does not prove
unattended classification, OCR, arbitrary font coverage or model quality.

Independent reviews covered runtime drivers, completion, recovery and cleanup.
Four PostgreSQL archives were fully decoded and hashed; globals and logs from
all nine containers were retained. The exact owned container set was checked
before fencing and destruction. All nine temporary services, volumes and network
were removed. Primary preservation checks pass before and after retirement;
the primary retains schema 70 / `mvp-91` and its original runtime.

Ignored evidence: `.cache/shuttle-work/pdf-font-consumer-20261022-v1/` contains
checkpoints, original failed-driver snapshots, review records, both rendered
PNGs, visual checks and `retirement/` archives/logs. Raw model requests and
responses remain in `.cache/shuttle-mock/pdf-font-r1/mailbox/`. Credentials and
source bytes stay local. No new product-code fix was required by this gate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
