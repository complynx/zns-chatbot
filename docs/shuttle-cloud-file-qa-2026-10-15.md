# Unknown-size cloud file refusal — 2026-10-15

An explicit Telegram getFile size refusal now reaches owner fetch/content reads
as HTTP 413 even when the persisted attachment size is unknown. The former
response was HTTP 503. No API, manifest, schema, dependency or financial policy
changed. Architecture and Go consultations preceded implementation.

## Classification contract

The validated Telegram failure envelope must have `ok:false`, error code 400,
HTTP status 400 or 200, and the exact description `Bad Request: file is too big`.
The existing decoder already permits coherent failure envelopes on HTTP 200.
Only ReadFile converts the private classification into its existing size-limit
sentinel. The API's shared reader already maps that sentinel to HTTP 413.

No raw description is stored in the error or returned to clients. Other bad
requests, near-match descriptions, rate limits, server failures, malformed or
incoherent responses keep their previous handling. Sending messages retains its
previous error type, status and delivery outcome. The new classification does
not grant access to any attachment or raise the local 20 MiB read ceiling.

Telegram's [getFile contract](https://core.telegram.org/bots/api#getfile) describes
the separate cloud download limit. Its official server
[source](https://github.com/tdlib/telegram-bot-api/blob/master/telegram-bot-api/Client.cpp)
contains this exact size refusal; the current source was inspected on this date.
Unknown variants remain unavailable rather than being guessed from a substring.

## Behavioral verification and review

Before the fix, the new Linux race test failed for both explicit refusal forms:
expected the size sentinel, received the generic sanitized status-400 error.
The other classification boundary cases passed. After the fix, focused Linux
race checks passed: Telegram 1.038 seconds, PostgreSQL integration 3.069 seconds.
Owner fetch and content returned 413, foreign access 404 and revoked access 403.
Authorization denials made no metadata requests. An unrelated source refusal
still returned 503 on both read routes. Existing successful exact-limit and
unknown-size small-body reads remained covered.

HTTP tests separately assert no binary download after refusal and no raw
description or token in errors. They also cover HTTP 200/400, near matches,
429/500, conflicting status/code, missing fields, malformed JSON and unchanged
send-error behavior. Strict lint for Telegram, API and integration reported zero
issues. The complete Telegram-client Linux race suite also passed in 1.125
seconds, covering the shared rejection classifier beyond file reads. Fresh
independent correctness and adversarial quality reviews found no
actionable findings. No new full integration or GitHub CI pass is claimed.

## Owned native before/after

The probe reused the already-uploaded synthetic 21 MiB text document with opaque
ID `c818d770-29a8-4d50-99f7-d487d8ccf6af`. A direct real Telegram getFile call
returned HTTP 400 with the exact refusal above. The owned database reference
normally records 22,020,096 bytes; the probe conditionally changed only that
fixture's size to zero and restored the exact old value in `finally`.

With unknown stored size, both owner API routes returned 503 on the previous
image and 413 on the candidate. This deliberately exercises the remote-refusal
branch; it does not claim Telegram naturally omitted size on this upload.
The same real local Zitadel owner was used. No new upload, Telegram message,
Loom run, payment, avatar or profile change was required. This was direct API
acceptance, not another Loom broker run or unattended attachment interpretation.

Only the owned bot was recreated, using image
`sha256:315269a74e66c9d4129d428dfabefcf9c8ab35afb4e95aae095d98f445c93bd2`.
The 329-path source freeze and exact equality of compiler output and the image
binary were retained. The bounded Linux build exited 0 without OOM; its scratch
QA image retained accepted certificates, avatar executable and notices. This is
recorded build provenance, not a second independent reproducible build or a new
production Dockerfile acceptance.

All 27 selected business/configuration/audit hashes, four runtime fingerprints
and the complete attachment-table fingerprint were preserved. Configuration
file hashes, schema 31 and manifest `mvp-30` matched. Loom kept its exact container
ID and start time. The bot remains on the accepted candidate on the owned stand.

Review of the ignored native helper found that restoring a fixture could be
mistaken for accepting the response. Its gates were corrected before candidate
rollout: response and preservation checks must succeed before phase acceptance,
and final success requires the accepted after phase. A fresh helper review found
no actionable findings. The original before evidence remains intact.

## Evidence and remaining scope

Ignored proof: `.cache/shuttle-live-test/cloud-file-qa/checkpoint.json`;
current runtime marker: `.cache/shuttle-live-test/cloud-file-runtime-final.json`.
Source freeze, binary provenance, lint and build evidence are under
`.cache/shuttle-work/cloud-file-qa/`. Linux test logs/terminal states are under
`.cache/shuttle-work/container-results/zinusya-cloud-file*-qa-20261015.*`.
The expected failing pre-fix test is retained. Its terminal failure is regression
proof, not a successful QA run.

Natural missing-size updates, other cloud errors, unattended classification,
production retention/limits and full shutdown/tracing acceptance remain separate
gates. Earlier [known-size acceptance](shuttle-file-limits-qa-2026-10-15.md) and its
original failure are unchanged.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
