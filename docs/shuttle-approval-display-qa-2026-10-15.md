# Telegram complete-parameter approval QA, 2026-10-15

This bounded display/rejection check passed in actual Telegram Web using the
current account and its existing private chat with `@complynx_test_bot`.
Local Zitadel, Loom and Zinusya were real. The provider was manually driven by
Codex and the approval's leaf RPC service was an explicit synthetic fixture.
This is not unattended interpretation or a real administrative business write.
The preceding real event-write acceptance remains recorded
[separately](shuttle-event-change-qa-2026-10-15.md).

## What was exercised

One signed Telegram message requested the disposable
`zinusya_qa_approval.display` operation. The driver bound the exact received text,
raw update, sender, private chat, message ID, task and run before responding to
the current model step. It did not create a replacement run or resend the message.

Loom prepared the complete synthetic arguments and Zinusya delivered two actual
Telegram messages. The outgoing payloads respected the 4,096 UTF-16-unit limit,
used no parse mode, and contained only `pre`/`code` entities. The first chunk had
no buttons; both buttons followed the final chunk. The visible browser `pre >
code` text was concatenated and parsed: both fields and the entire string matched
the original arguments exactly. Backticks, Markdown stars, an HTML-like fragment,
emoji and `[Daniel](tg://user?id=8968148043)` remained literal. The code blocks
contained no active links. Begin/end canaries survived across both messages.

The Reject button was clicked in the actual Telegram client. Its durable callback
matched the expected callback data, sender and target task. The same Loom
operation became `rejected`; its exact tool result was `{"status":"rejected"}`.
The original run and task completed, and the bot's final rejection reply appeared
in Telegram. The fixture recorded zero execute attempts.

Twenty-seven selected business/configuration/audit tables retained their
baseline hashes, including profiles, registrations, passes, pairs and payment
records. Runtime intake/task/conversation/outbox and user identity/token state were
outside this comparison; message processing changes runtime and authorization
state. This is scoped preservation, not an assertion that the whole database is
unchanged. No IAM role or caller permission was granted. The fixture advertised no Caller
stanza, and the Loom configuration bytes remained unchanged.
The Zinusya image remained
`sha256:fa34fa4e2d14a37b951ccd8b6a5b8fa265c57fe42dc0f02ac8b59406553ce321`,
with schema 31 and manifest `mvp-30`.

## Fixture and recovery boundaries

[The fixture](../deploy/shuttle/test/approval_fixture.py) uses Python's standard
library. It advertises one static exact-parameter method with no Caller stanza,
accepts only its preselected synthetic arguments, preserves repeated preparation
and unconditionally refuses execution. Loom performs user authorization; the
fixture itself checks only bearer presence and exact synthetic data. It is a
test leaf, not an authenticated business API.

It ran as a nonroot user with read-only root/source/arguments, a separate writable
journal, 64 MiB/no swap, 0.5 CPU and 32 PIDs on `shuttle-live-test_default`. No host
port was published. Its exact saved container ID and labels were checked before
removal. The journal, original proposal evidence and screenshots remain local.
The fixture stop used Docker's three-second bound and ended with exit 137;
`OOMKilled` was false. This is a fixture stop fallback, not evidence of graceful
mock-server shutdown or a Zinusya shutdown failure. The temporary container was
removed; no extra database or volume was created for this scenario.

The actual Telegram message ID was 4224. The saved run was
`de13b8d8-61b9-455a-9df9-66d3de41fb91`, operation
`73f9ba82-cddb-4f57-987c-1ad19bac57a3`. Evidence is held locally in
`native-approval-display-checkpoint.json`, the fixture journal and browser DOM
verification under the ignored stand directory. Before/after screenshots are
`confirmation-before-reject.jpg` and `confirmation-rejected.jpg` there.

The existing approved Go source freeze and 242-scenario race gate were unchanged;
this slice added test tooling and actual browser acceptance. Python syntax,
actual discovery/preparation and the full Telegram flow exercised the fixture.
Fresh independent safety/quality reviews passed. Driver review strengthened
message binding, literal-display assertions and the final-response submission
guard. A bounded readiness retry fixed a startup race in the local launcher.
The staged diff remains empty.

The first driver attempt failed before any tool submission: it incorrectly
projected the normalized inbox as a raw Telegram update. The corrected projection
uses nested `raw_update`. Telegram Web also collapsed the draft blank line; both
the draft and exact received text are retained. No second message was sent, and
the same original run continued. These are retained QA harness failures, not
reported Loom bugs.

The old inline keyboard remains visible after rejection. Backend stale-prompt
checks protect it; keyboard retirement remains a separate usability improvement.
This check does not establish all native callback races, restart delivery,
actual administrative approvals from a Telegram administrator, CA/zitadeltg,
production deployment or production readiness.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
