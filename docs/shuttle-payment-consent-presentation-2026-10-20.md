# Payment consent presentation, 2026-10-20

Status: component checks, source review, packaging and the separate
[owned IAM/Loom runtime gate](shuttle-payment-consent-runtime-qa-2026-10-20.md)
passed. The subsequent [primary installation and binary rollback](shuttle-payment-consent-primary-qa-2026-10-20.md)
are also accepted. The owned primary now runs this fix at schema64/mvp78, with
eleven services and the original 134 Loom runs preserved.

## Problem and change

The [agent sales-closing gate](shuttle-pass-agent-closing-runtime-qa-2026-10-20.md)
observed complete human payment terms followed by raw workspace/review JSON.
The generic Telegram approval handler appended arguments. With an abbreviated
Loom description it could also omit the stored human terms.

`zinusya_payments.review_commit` now reads the original actor-owned transport
operation and immutable preparation. Strict argument decoding, recipient/principal
binding, current event authority, exact argument equality, availability and an
expiry bound precede delivery. The existing Telegram entities, ordered chunks,
last-chunk reply keyboard, operation/hash identity and free-text response path
remain in use. Showing the question neither confirms nor changes money.

Completed agent transport receipts now retain their original preparation. The
same preparation is returned when another interface completed the canonical
decision before that transport. The question explicitly says the decision already
completed and offers continuation to reconcile its original result, not a new
payment. The existing idempotent commit recovers one financial effect. Native
completed transport reads retain their existing result path.

## Verification and limits

The focused PostgreSQL gate passes 36 test roots / 59 cases, with no skips. It
includes payment review workspaces, mini-app review handoffs and pair approval
presentation. Seven new cases cover English/Russian delivery, changed amount,
extended expiry, revoked authority, and native/agent completion before delivery.
They assert original human terms, absence of technical argument JSON, an unchanged
pending balance and exactly one completed credit of 30.87 BYN after replay.

The task, API and database package checks pass. The full repository linter reports
zero issues, and a Linux amd64 binary cross-build passes. These are host component checks, not full Linux/race or browser acceptance.
Two independent source reviewers found no actionable issues in the final round;
the Git index stayed empty. Source and binary hashes are recorded in the ignored
`payment-consent-v1/source-review-final.json` evidence directory.

The initial negative tests incorrectly waited for River errors, then only for run
reads. The service instead snoozes after a presentation error and may retry the
idempotent run creation before saving its ID. The final tests observe repeated
create/read attempts and drain workers before checking that no consent arrived.
The failed attempts remain in local evidence and are not labelled passes.

A wider first run with the disposable PostgreSQL data in a RAM-backed mount hit
its 512 MiB memory limit. Docker recorded OOM and PostgreSQL entered recovery.
Diagnostics are retained. That exact container was removed; the successful gate
uses a fresh, bounded PostgreSQL with disk-backed data. Both disposable test containers and their temporary storage are now removed; the
final one stopped with exit zero and no OOM. No temporary test schemas remain. The
primary was independently checked unchanged. This infrastructure failure is not a product recovery test or a passing suite.

The separately linked runtime gate verifies the packaged binary, original payment
question, untagged consent, completed receipt, exact credit and actual Telegram
notifications. Its three databases are archived and all nine temporary services
removed. Native-completed runtime recovery, full Linux/race and real WebView
acceptance remain separate; this source gate does not claim them.

The separately linked primary installation gate now passes, including rollback,
HTTP/auth readback and stronger preservation checks for Loom contents/schema
definitions. The remaining expiry/long-outage, unattended-model, real WebView and
full Linux/race gates are separate. No production readiness or deployment is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
