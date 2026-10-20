# Payment consent runtime QA, 2026-10-20

Status: packaged candidate accepted on one isolated owned stand; its three
databases are archived and nine services removed. This runtime gate left the
primary on its preceding schema64/mvp78 image with eleven services and 134 Loom
runs. The subsequent [primary installation and binary rollback](shuttle-payment-consent-primary-qa-2026-10-20.md)
now promote this exact accepted image in a separate gate.

## Candidate and scope

This gate checks the [payment-consent source fix](shuttle-payment-consent-presentation-2026-10-20.md).
The candidate uses the exact reviewed Linux binary on the preceding primary image.
Its source digest is `45646c3a82196c4636520bbffd92cf23ec643c2a4f01c875c069af960002c670`, binary digest is `d37e1ca575865d4d71adfc66d19abb397c3d296178781147c4dce8748f89d509`, and image is `sha256:9ea106cda26f7d0e58674de3dfd611c35655711b08783914f17c2b2bf7b9a68a`.
The binary extracted from the built image matches the reviewed binary. Image
configuration and base layers are preserved. The temporary extraction container
was never started and was removed.

The manifest remains `zinusya/mvp-78`, schema64; the image digest distinguishes this
fix from the preceding primary image. No migration or primary replacement occurs
in this gate. Real owned Zitadel and Loom run with a controlled provider and
synthetic Telegram. Four fixture identities are bootstrapped; only the owner and
event ambassador participate in the two agent runs. An assigned pass priced at 100 BYN with a
30 BYN deposit is a prerequisite, not another full registration acceptance test.
The reused fixture event name contains "closing deadline QA"; this gate does not
claim another closing/deadline or restart scenario.

## Observed behavior

The owner sends one original PDF (635 bytes) with its exact caption through synthetic
Telegram. Its opaque file ID and caption reach the actual Loom model input. The
actual payment tool records one pending proof and credits nothing. The ambassador
fetches that original proof through the protected file tool; its SHA-256 and size
match the original upload.

The actual pending payment operation delivers one Telegram message containing all
original stored human terms: payer/event, reported and credited 30.00 BYN, original
and remaining balance, deposit requirement, original proof time and deadline in
Europe/Minsk, reason and proof reference. Manual inspection of the saved actual
transcript confirms the readable text, literal `pre` entity and normal
Approve/Reject reply keyboard. No raw workspace/review argument JSON is appended.
The keyboard belongs to the delivered question; no hidden response tag is required.
Actual receipt chat/message IDs, text, entities and keyboard equal the intended
outbox payload. The final-chunk rule is checked; this question fits one chunk.

One untagged free-text consent reaches the actual pending Loom dialogue. Resolution
uses its original approval ID and argument hash, completes the original write and
produces exactly one ambassador decision. The actual `review_operation` tool then
recovers the completed canonical result and original preparation, including its
exact arguments and full human description. Two Loom runs complete: submission and
review. Balance is 30.00 BYN confirmed / 70.00 BYN remaining; deposit satisfied, pass
not fully paid.

Three payment notifications are actually delivered: submitted-proof notices to the
administrator and ambassador, then confirmation to the owner. Every receipt is
matched to exactly one message in the correct chat with the expected text/entities;
no duplicate notice text is found for its recipient. All bot tasks, outbox entries
and payment notifications reach their completed/delivered state before cleanup.

## Review, retention and limits

Fresh ordinary and adversarial reviews took three rounds. Two QA gaps were fixed
before any runtime execution: notification DB flags alone did not prove actual
delivery, and persisted markup alone did not prove the actual delivered keyboard.
The final reviewers found no actionable issues. Product code and the Git index
were unchanged during this gate. Packaging, runtime and cleanup completed without
a runtime failure or retry; review drafts were not executed or labelled passes.

Cleanup stops all possible writers before archiving Shuttle, Loom and Zitadel.
All three custom-format archives are fully decoded by `pg_restore`; byte counts
and SHA-256 digests are retained. All nine services stop with exit zero and no OOM.
Exact owned project membership is rechecked before removal. Its containers, networks
and volumes are gone; the primary's original domain data and runtime remain
unchanged. This is archive validation, not a fresh restored-service acceptance test.

Evidence is retained under ignored
`.cache/shuttle-work/payment-consent-runtime-v1/`: `runtime-acceptance.json`,
`approval-delivery.json`, `manual-observation.json`, final transcript,
`cleanup-proof.json`, private logs and three private database dumps. Source/binary
packaging evidence is in `payment-consent-v1/image-proof.json`; final script review
is `payment-consent-runtime-review-v1.json`.

This gate does not cover real Telegram/WebView rendering, unattended model
interpretation, the native-completed runtime variant, token expiry, long outages,
restart/fault recovery or full Linux/race checks. Completion through either interface before question delivery has separate
component coverage. Production readiness and deployment are
not claimed. The separately linked owned primary installation and binary rollback
are now accepted; they do not expand this runtime gate's business coverage.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
