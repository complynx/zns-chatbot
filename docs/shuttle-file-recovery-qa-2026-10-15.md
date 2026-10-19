# File source and temporary-artifact recovery QA — 2026-10-15

This acceptance closes a recovery evidence gap. Architecture and Go consultation
found that the existing runtime already refreshes Telegram paths on every read,
preserves durable metadata after source failure and rechecks authorization.
No production code, API, schema or dependency changed.

The existing PostgreSQL integration scenario now checks both fetch and content
failure, restarts the service while the source is unavailable, restores the source
and reads the same UUID through the restarted API. It compares original metadata,
descriptor fields, bytes and SHA-256. Error responses contain neither artifacts,
file content nor private Telegram IDs or credentials. Existing foreign/revoked
access assertions remain. Review found that the first test revision accidentally
kept the old content handler after restart; the final version creates a new
internal HTTP server and authenticated request using the original descriptor path.

The final focused Linux race run passed: integration 2.048 seconds and Telegram
client checks 1.031 seconds. Strict integration lint reported zero issues. The
initial lint findings and the pre-fix handler coverage limitation are retained
in the local evidence. The earlier full 256-scenario gate predates these added
assertions; it is not described as a fresh complete run of this test revision.

Native acceptance used the unchanged owned schema-32 / `mvp-31` executable:
`sha256:7c70a34388bd72e603293f9050fe84199f1aa3cdd5d95a6155eadc98478252bb`.
No Telegram message, upload, service restart or authorization change was needed.
The existing synthetic video belongs to the authorized test account and was
previously received through real Telegram. Its size is 70,241 bytes and SHA-256 is
`196fa8f5068be9f992198238b94f7ea2c7e4241ff25dfd6153a4fcf8d8391d42`.

The harness first checked owner info/fetch/content and exact bytes through real
IAM and Telegram. It then temporarily replaced only that synthetic reference's
private Telegram source ID with a nonexistent test ID. Both owner read routes
returned HTTP 503; metadata stayed identical. The real Loom script broker also
returned `module HTTP status 503`. This is a controlled invalid-source refusal,
not a claim that Telegram deleted the original file or that its service was down.
The source ID was restored conditionally in `finally`, preserving the full row.

An explicit fetch in the same Loom run then imported the original bytes without
new intake. The first native observation stopped because a shared QA decoder
discarded `artifacts` while unwrapping the broker's `result` field. The complete
successful tool result already existed in Loom. The corrected harness read that
exact saved result, preserved the failed observation and continued the same run;
it did not repeat the completed import or resume a paused run.

Only the newly imported test artifact was then explicitly expired. Its direct
content endpoint returned HTTP 404. Another explicit fetch of the same durable
Zinusya UUID created a different temporary artifact with exactly the same size
and SHA-256. Both imported versions were read through Loom's authenticated
content API. The run completed normally using the manual provider. The remaining
new test artifact was marked expired after verification; physical deletion by
Loom's collector was not part of this acceptance. Preexisting artifacts were not
expired or deleted by the harness.

All selected business/history/config table hashes, prior task/inbox/outbox rows,
user rows and the original file row were unchanged at completion. Runtime image
and production-source hashes matched the prepared baseline. The disposable test
PostgreSQL stopped with exit 0 and no OOM and was removed; completed test runners
were removed after their results were saved.

This proves explicit authorized source retry and reimport after temporary-artifact
expiry. It does not establish automatic model retry, unattended attachment
classification, actual permanent Telegram deletion, elapsed real-world TTL or
production retention policy. Source refusals remain unavailable responses rather
than an invented permanent HTTP 410 classification. Telegram documents temporary
path renewal through [getFile](https://core.telegram.org/bots/api#getfile).

Ignored native evidence is `.cache/shuttle-live-test/file-recovery-qa/checkpoint.json`;
focused test and terminal container evidence is under `.cache/shuttle-work`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
