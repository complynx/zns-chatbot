# Zinusya consumer tracing acceptance, 2026-10-22

The bounded consumer gate passed Telegram intake → durable Zinusya task →
actual Loom queue → script/module RPC → Zinusya HTTP, including consumed steering
and a graceful bot restart. It used synthetic Telegram and a controlled mailbox
provider, not a real Telegram session or a paid model.

## Runtime and behavior

The owned `iam420-zinusya-v1` fixture used bot source `74604b7e` / image
`0d4457b6`, schema72 / `mvp-92`, published Loom `b2b8d429` / image `9ddd3175`,
and Zitadel4.20.0 / image `9ea0e9ec`. Its prior synthetic IAM app-projection
repair remains documented in the [IAM report](shuttle-iam420-compat-qa-2026-10-22.md).
The primary/base installations and the Loom developer's resources were unchanged.

An existing synthetic user sent a profile-read request. While the first model
request remained pending, another message supplied steering. The input was saved
but unapplied, and Loom's applied-parent carrier stayed empty. The bot then
stopped by SIGTERM in0.578 seconds and restarted; the original task and run
continued. No second run was created.

Three controlled provider answers requested two read scripts and supplied one
final reply. Loom cancelled the first script as stale after steering; only the
current script executed and returned the expected user's profile/version.
Two requested scripts must not be described as two executed reads. The run
completed and exactly one matching final outbox message reached `delivered`.

## Exported trace evidence

The bounded private OTLP/HTTP receiver retained139 spans. The exported graph
contains both Telegram admission spans, Zinusya task-attempt links, the saved
Loom origin carrier, and the consumed steering carrier. Two origin-trace and
six steering-trace `execution_step` spans were observed. Graph validation
requires the referenced carrier spans to exist in the export; matching a
carrier ID alone is insufficient.

After consumption, the applied input ID and parent matched the original steering
record. Subsequent execution used that trace and retained a causal link to the
original run admission. A separate readback narrowed the downstream proof to
the one executed RPC: its `module_rpc` span and the bot's HTTP200 POST span share
the exported `script` parent; that script has an exported `execution_step`
parent in the steering trace. The HTTP interval falls inside the RPC interval.
The RPC span records the interval as a sibling of the HTTP span, not its parent.

## Retained failures and cleanup

The first controller placed Loom's `otel` block at the YAML root; strict loading
rejected it before a message was submitted. The second stopped during local
configuration preparation because an exact section guard did not account for
CRLF. Neither attempt dispatched a message or model request. Both original
receipts remain. The third normalized line endings and inserted
`observability.otel`; its exact configuration transformation was checked on
copies of the actual files before runtime execution.

Independent reviews corrected cleanup independence, explicit reporting of an
unknown collector-creation outcome, and dangling-link acceptance in the trace
validator. The final pre-run review had no actionable findings. All nine fixture
containers stopped with exit0 and without OOM. The original two configurations
were restored byte-for-byte. The collector stopped with exit0/no OOM and was
removed; its bounded journal and inspect evidence remain.

Private evidence: `.cache/shuttle-work/consumer-trace-20261022-v1/`,
`consumer-trace-20261022-r2/`, and `consumer-trace-20261022-r3/`. The accepted
receipt is `r3/checkpoint.json`; `r3/rpc-readback.json` records the narrower
executed-RPC evidence. A readback initially assumed a direct execution-step
parent; inspecting the actual graph identified the intervening script span.
No workflow was repeated for that correction.

## Remaining scope

This proves one controlled read dialogue, steering consumption and bot-process
restart on the separate fixture. It does not prove a Loom-process restart in
this consumer path, approval continuation, delayed-input permutations, collector
failure, primary trace-backend visibility, model judgment or production readiness.
The independently rerun [Loom source tests](shuttle-loom-durable-tracing-qa-2026-10-22.md)
cover additional restart and approval cases; they remain separate evidence.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
