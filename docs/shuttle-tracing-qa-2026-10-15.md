# Tracing and authorization recovery QA — 2026-10-15

Two application defects were fixed during bounded QA on our owned stand.
The work does not establish complete tracing acceptance or production readiness.

## OTLP endpoint

The rollout guide supplied a collector origin such as `http://alloy:4318`.
The actual Zinusya exporter sent POST `/` for this configuration. A behavioral
test reproduced it before the correction. Empty and root URL paths now use
`/v1/traces`; an explicit custom path is preserved. Export remains optional.

The test observes the actual HTTP method, path and protobuf content type, decodes
the export and checks its span. Linux race checks for observability passed in
1.577 seconds and the final test version in 1.510 seconds. The Windows race command
could not run because CGO was disabled; it is not counted as a passing gate.
Strict lint passed after adopting protobuf getters. No dependency was added.

## Native trace evidence and interrupted scenario

One signed request from the authorized account in @complynx_test_bot used actual
local Zitadel, Zinusya and Loom with a manual scenario provider. It requested two
read-only profile calls around a bot restart. Personal profile fields were not
included in the tool results or final reply. A private bounded OTLP receiver had
no published port, 96 MiB memory/no swap, two concurrent trace decodes and a
4 MiB/256-batch journal cap.

Before the interruption, decoded exports proved:

- Persisted Telegram acceptance context linked to `task.process` attempts.
- A task attempt as the parent of Loom's HTTP run-creation span.
- An active Loom `script` span as the parent of Zinusya HTTP API spans.

The API parent is the active script span. An initial QA assertion incorrectly
expected the retrospective `module_rpc` span; the observed parent relationship
was recorded instead, without repeating tools. Exported span attribute keys were
bounded, and the test request marker and bearer prefix were absent from the
journal. This is a scoped inspection, not a general proof about every span.

The receiver then returned actual HTTP 503 responses. The bot stopped with exit
code 0, no OOM; the measured Docker stop command took 1,086 ms. This is below the
five-second ceiling, but it does not prove the one-second target. After restart,
Loom reported `waiting_auth/authorization_required` before the second tool.
Zinusya kept polling that HTTP-200 result without renewing its unexpired session.
The original outage/restart acceptance failed at this point.

The explicit receiver fault was removed during diagnosis. Continued polling
eventually reached the receiver's 256-batch cap; it correctly became unhealthy
with HTTP 507. Its journal and failure evidence were retained. The receiver later
stopped with exit 0/no OOM and was removed. Original Loom/Zinusya configuration
files remained byte-for-byte unchanged; temporary mounts/environment were removed.
Do not claim that the second profile call completed while the receiver returned
503, or that the original full tracing/outage scenario passed.

## Recovering the original run

Zinusya now reauthorizes the existing auth session for the exact observed
`waiting_auth/authorization_required` state, even when its cached expiry is in
the future. It obtains fresh subject credentials and preserves the existing
non-idempotent renewal uncertainty gate. Loom's session reauthorization queues
the original waiting run; its Resume endpoint does not support this state.

On the same native task, session version increased from 1 to 2. The task, session
and run IDs stayed the same. The saved second tool completed once, followed by
the original task and one final reply. Selected 27 business/configuration/audit
hashes remained equal. Runtime and identity bookkeeping are outside this hash
comparison. This recovery used restored telemetry configuration and is separate
from the failed outage acceptance.

Behavioral race checks cover unexpired renewal, lost renewal receipt across
restart, local and explicit permission revocation, the unchanged deadline across
successful renewals, and deadline expiry before another mutation. Review also
found that a create/replay response in `waiting_auth` had to save its known run ID.
Its test failed with an unnecessary third CreateRun request before the pin was
added; afterward recovery uses fresh GetRun. The final selected recovery suite
passed in 17.857 seconds. These are focused checks, not a new full-suite pass.

The native original-run recovery used image
`sha256:b1456cb4d90e24e4a1b9aa19a705f949099d49e7feb7f4a20794662aea1ed00a`.
The subsequent run-ID pin is covered by behavioral fault injection; this report
does not claim a second native lost-create injection.
The final image
`sha256:295cb68a4e3cce29179979ea305552535d98b80873ff9ff1ee802ca6fe2a9470`
was installed afterward. A real IAM owner API read, preserved original completion
and unchanged business hashes passed after that bot restart. Schema 31 and
manifest mvp-30 remain unchanged.

## Separate corrected-image retry: discovery pause

A second signed Telegram request used the final `295cb68a` image and a separate
receiver, task and journal. The earlier failed scenario and its evidence were
retained. The first real profile read succeeded. Actual trace exports and HTTP
503 rejection were observed before stopping the bot. Docker stop took 947 ms;
the bot exited 0 with no OOM. This single observation is below the one-second
target; it does not establish a general shutdown SLA.

After restart the same auth session advanced to version 2, but Loom paused the
same run with `module_discovery_unavailable` before a second result was available.
The driver timed out without submitting another script or final reply. This
second automatic outage/restart acceptance also failed. It does not prove that
the second read succeeded during receiver failure or that exports recovered in
the intended automatic scenario.

In the tested Loom source, every tool-catalog construction error maps to this
pause reason, including a repeated authorization failure, transport errors,
HTTP 403 and invalid catalogs. The specific underlying error was not captured;
transient startup is a hypothesis, not an established cause. Existing revocation
QA also demonstrates a deliberate owner-403 pause with the same reason.
Independent architecture and Go consultations therefore rejected broadening
automatic Resume beyond the existing agreed states. Safe automatic recovery
needs a Loom contract that distinguishes temporary discovery failures from
authorization denial and invalid catalogs.

The intentional receiver fault was removed during diagnosis. The receiver later
reached its 256 accepted-batch cap and returned HTTP 507; this is retained as a QA
limit. Its 304,097-byte journal was preserved. Original configuration mounts and
environment were restored, and only the exact temporary receiver was removed
(exit 0/no OOM).

A fresh owner API read then succeeded. An explicit operator Resume, with its key
persisted before dispatch, recovered the original task/run. The saved second
script was not resubmitted. Both tool calls have exactly one completed tool
record, the original task and run completed, and exactly one final outbox entry
was delivered. Selected 27 business/configuration/audit hashes remained equal.
This is operator recovery, not automatic acceptance. Image, schema 31, manifest
`mvp-30` and the 323-path application source freeze remained unchanged.

- Retry task: `1e331625-6a72-43c0-b742-55042efb02c7`.
- Retry run: `f827b3ac-cc8e-48ad-b1ce-ee5774b5b221`.
- Ignored evidence: `.cache/shuttle-live-test/otel-retry-qa/checkpoint.json`.
- Separate final read-back: `.cache/shuttle-live-test/otel-retry-runtime-final.json`.

## Open trace boundary

Update, 2026-10-22: Loom #27 is implemented and published. Nine focused
Linux/race/PostgreSQL/OTLP tests passed independently; see
[the new source acceptance report](shuttle-loom-durable-tracing-qa-2026-10-22.md).
Primary image installation and full Zinusya consumer acceptance remain open.
The following source observations describe the earlier version.

Read-only source revalidation on 2026-10-21 found local Loom still at
`a958185e8f81baf49a9cbee492adebd511518995`. In `internal/app/runtime.go`,
`executeRun` retains only `run_id`, the queue insertion supplies that locator,
and the worker starts `run_execution` without restoring a saved admission
carrier. This agrees with the open boundary below; it is static source evidence,
not a new runtime trace test or a statement about the production Loom image.


The copied Loom version explicitly starts a fresh async run trace without
persisting the admission trace context. Both HTTP boundaries can propagate
correctly while the graph remains disconnected through the queue. Full causal
tracing requires a Loom-owned change and separate acceptance; the developer's
checkout and stands were not changed. Missing telemetry during collector failure
is permitted: trace export is best effort, not a durable business ledger.

Ignored evidence: `.cache/shuttle-live-test/otel-native-qa/checkpoint.json` and
`otel-native-qa/state/exports.ndjson`. Application source freeze:
`.cache/shuttle-work/waiting-auth-source-freeze.json` (323 paths).
Final bounded authority: `.cache/shuttle-live-test/authorization-tracing-runtime-final.json`;
its full-trace acceptance flag remains false.

- Original task: `d88da099-7f60-4d5d-9765-ab8e96381624`.
- Original run: `d1c6448b-18aa-412b-8e45-a1c8a6dbbed8`.
- Original auth session: `5a54e26e-6449-4f0b-95a6-51e530779e19`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
