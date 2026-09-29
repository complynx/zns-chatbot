# Agent diagnostic log

Diagnostic events reuse the JSON logger and its current OTel trace context. They
do not contain memo text, tool arguments/results, user messages, credentials or
owner IDs. Domain receipts remain the authority for committed actions. A log
entry is not proof of a business transaction.

`observability.NewAgentEvents(logger, correlation, attempt)` creates one attempt
recorder. The caller supplies a host-generated canonical UUID and a positive
attempt number. Persist the UUID with the input across retries; increment the
attempt for processing again. `Emit` returns a sequence for child `Parent`
references. A cached outcome uses `Replay: true` and outcome `replayed`; a second
execution gets its own attempt. A process crash may leave `started` without a
terminal record. Treat this as incomplete evidence.

The schema uses finite phase, operation, outcome and error categories. Unknown
strings become `unknown`; they are never echoed. Durations, byte counts and
result counts are bounded. A recorder writes at most 256 events, reserving its
last event for a `limited` marker. The logger adds trace/span fields when present.
Events are JSON strings in the `agent_event` attribute. This avoids passing
arbitrary structs through the logger's private-value handling. Record size fits
its 4096-byte text bound. No metric labels are added.

The operation vocabulary includes model settings, profile/language, massage and
broadcast tools. Exact public dotted names survive the logger's JWT filter;
configured secrets are redacted first. Unknown names and suffixes do not gain
permission through a matching namespace. Recording and export use the same
finite vocabulary, so a known tool remains identifiable during retry analysis.

## Code evidence

`EmitCode` accepts the proposed function body and derives evidence with Sobek's
parser; it never evaluates the code. Source-map loading is disabled. All literal
values are replaced; arbitrary identifiers are renamed. Only finite known
collection/tool names are retained. Syntax gaps must contain allowed JS keywords,
operators and whitespace. Supported ordinary code retains its control flow and
calls such as `filter`/`map`, making repeated glue code inspectable.

The implementation deliberately does not write a second JavaScript parser or a
complete pretty-printer. Comments, unsupported spellings, excessive structure,
or a redacted source exceeding 1024 bytes produce structural evidence only.
Template literals are replaced as a whole. That loses their internal expressions.
The outline keeps at most 32 notable node categories and 32 known method names;
node traversal is bounded. Invalid or oversized source produces `omitted`.
Original code and parser diagnostics are never fallback values. The retained
source is diagnostic text, not a script to execute or a fidelity guarantee.

This is a strict content-minimizing sanitizer, not a claim that regex detects
arbitrary secrets. Memo contents, including memo copied into code literals or
identifiers, must not enter these logs. Existing private execution receipts are
not copied into this diagnostic stream. A source-free outline is an explicitly
marked limitation, not full code logging parity.

## Operator export

Build with `go build ./cmd/agent-log` from `platform`, then run:

```powershell
./agent-log.exe -file ./operator-log.jsonl -since 2026-09-26T10:00:00Z -until 2026-09-26T11:00:00Z -limit 100
```

Records go to stdout as JSONL. A manifest goes to stderr with `records`,
`skipped`, `next_offset`, `incomplete` and `reason`. Continue with `-offset` using
the same unchanged file and filters. Timestamp bounds are inclusive/exclusive.
The byte cursor is not transferable across rotation or replacement: use a local
immutable log snapshot for repeatable analysis. The command reads only its
explicit operator-provided file. It has no database or network access.

Each call scans at most 8 MiB and returns at most 1000 records; a line may not
exceed 64 KiB. Unfinished trailing lines are not exported and retain their start
cursor. Invalid/unrelated lines are counted as skipped. Only recognized event
fields survive export; metadata and code evidence are sanitized again. The tool
does not export arbitrary log lines even at debug level. File read and write
errors do not echo filenames or contents. A nonzero exit can follow partial
output, so check both exit status and manifest before analysis.

Keep files and exports operator-only, with existing log rotation/retention. This
CLI grants no application rights and is not a public bot tool. An analyst model
can compare recent chains for repeated validation failures, result limits,
timeouts and verbose glue. It must label inferred causes as hypotheses: empty
searches, pagination and legitimate permission denials are not inherently agent
failures. Export omissions and unfinished chains limit conclusions.

## Runtime integration

After authenticating an update, the bot stores a random correlation UUID and an
incrementing attempt in the private `bot.interactions` diagnostic_context record.
This record bypasses conversation archival and is not model input. A one-second
diagnostic database deadline bounds this optional work. Failure writes a fixed
omission notice and does not prevent business handling. Directly constructed test
bots with no configured logger skip this diagnostic work.

Request events surround authenticated processing. `model.loop` is one logical
planning round; `model.skills` and `model.plan` are separate actual structured
provider invocations after authorization and input projection. OpenAI and Codex
share that boundary. `model.remote` identifies Remote's single opaque request;
its internal provider work is unknown. A loaded final-plan cache emits
`model.cache` with replay=true; attempt>1 alone never implies a replay.

Script events include sanitized code, and nested business/discovery events include
only sizes and outcomes. Empty script list results are `no_results`. Script
failure can follow successful tool effects: these remain separate committed
domain receipts, never inferred rollback. Known unavailable/invalid/result-limit
branches have fixed categories. Other errors are generic rather than copied from
the worker or provider.

This slice covers planning and script calls. Other independent summarization,
knowledge assessment and manual domain paths are not yet individually represented
in this event stream. Their request parent and existing OTel measurements remain.
Independent Code QA and Functional QA acceptance are still required. No dashboard,
automatic struggle score, analytics database or JSON query dependency is introduced.
