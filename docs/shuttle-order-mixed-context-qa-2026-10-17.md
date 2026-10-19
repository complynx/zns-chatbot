# Owned mixed order review: context-pause checkpoint — 2026-10-17

## Result

Native-to-agent review acceptance did not pass. The native order and proof remain
intact; no payment review was executed. The earlier complete native-only flow
remains accepted within [its documented limits](shuttle-order-proof-native-qa-2026-10-17.md).

The owned runtime is schema 45 / manifest mvp-44, with real Telegram, Zitadel and
Loom fc6c5747a4359ff94a58071054e124ba1449cc89. The provider is a manually driven
mailbox. Neither this checkpoint nor a counted synthetic fixture proves unattended
model behavior. No upstream Loom source or external developer stand was changed.

## Retained failures and successful partial checks

A three-document Telegram batch produced three durable file references. Its shared
run paused with context_unavailable after a completed summary and two input steps,
before a model step. The intake observation timed out; one explicit operator resume
of the same run repeated the pause. The official cancel stopped that file-only run;
no file was uploaded again and the accepted references remained usable.

For the native-to-agent path, the Mini App then submitted a different unique proof
for the existing synthetic order, holding two resource units. The reviewer form
loaded that proof and saved an exact decision without confirming payment. A protected
real-IAM content read matched the original synthetic PDF hash.

An explicit Telegram request reached real Loom with the preceding native review
operation in its context. Broker reads returned the original pending evidence and
same prepared workspace/revision. The actual Telegram approval showed 25.68 BYN,
items and required charge, event-local times, reason, proof ID and resource effect.
No review existed before consent.

After an untagged exact free-text consent, that run also paused with
context_unavailable before interaction_resolve was dispatched. The observation
failed; the database still had zero payment reviews. The original workspace later
expired. Do not reuse that expired consent or mark the mixed path passed. Continue
only with reconciled state and fresh exact consent for any new preparation.

## Diagnosis limits and next step

Read-only source review confirms that input-only steps do not trigger active
compaction before a model step. Final preflight still enforces token/input bounds.
The mailbox provider has no token counter, so conservative bounds apply. Measurements
of component sizes do not establish the complete rejected request's bound. The
native ArtifactIDs count was zero, excluding the separate artifact fallback as the
cause in this run. An upstream bug is not yet proven; preserve this as an integration
observation pending complete preflight measurements or a controlled reproduction.

A separate test-only counted Responses/mailbox adapter is now activated on the owned stand:
`deploy/shuttle/test/counted_provider.py`. Its count is explicitly synthetic: one
unit per four canonical UTF-8 JSON bytes, rounded up. It is not model tokenization
or proof of real context limits. It provides a controlled seam for testing durable
business and interface behavior without deleting history or changing upstream code.
It does not implement native compaction. Nine local behavioral HTTP tests passed;
owned Docker configuration activation passed; mixed-flow and real-model acceptance remain pending. Review corrected
strict usage/output bounds and added an absolute body deadline. The first Windows
trickle test observed a TCP abort rather than HTTP 408; that failure is retained.
The final test accepts a bounded rejection/disconnect, rejects a client timeout,
and requires subsequent service after all four occupied slots recover.

The activation used the same frozen Loom image and a separate HTTPS fixture with
verified hostname and the existing CA trust. A nonroot container mounts a dedicated
TLS volume read-only; the original key permissions remain unchanged. A temporary,
network-isolated init container populated that volume and was removed. The fixture
has no host port and is limited to 128 MiB, no swap, one CPU and 64 PIDs.

Full Compose comparison, database backups and before/after hashes verified that
business records, history and immutable task bindings survived. Real IAM profile
and paused-run reads passed; the same ingress container resumed polling. The eight peer Compose services,
including the bot, were unchanged. No model response, run resume or
payment operation was requested by this activation. The expired consent remains
unexecuted. This proves configuration and recovery checks, not a provider request
originating from Loom or the failed mixed scenario.

Preparation failures remain in the ignored `counted-provider-main-cutover-v1`
through `v4` checkpoints. The HTTP endpoint attempt failed Loom startup and was
rolled back with durable hashes verified; later TLS key access checks failed
before any main-service interruption. The accepted activation receipt is
`.cache/shuttle-live-test/counted-provider-main-cutover-v5/checkpoint.json`.
Browser-origin approval is pending before further Telegram UI QA.

Ignored evidence is retained under
`.cache/shuttle-live-test/order-notice-real-qa-v1/`: `checkpoint.json`,
`three-file-intake-timeout-v1.json`, `three-file-context-observation.json` and
`mixed-review-context-failure-v1.json`. The handoff draft is
`.cache/shuttle-work/loom-three-file-context-draft.md`; it has not been published.
No production deployment or readiness claim is made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
