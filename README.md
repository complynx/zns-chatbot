# zns-chatbot
Zouk Non Stop chatbot

The bot is Зинуся / Zinusya. Shuttle is the codename of the Go rewrite.
See [naming and rollout compatibility](docs/shuttle-naming.md).
See [developer workflow](docs/shuttle-run.md), [avatar tools and limits](docs/shuttle-avatars.md)
and the [behavioral QA procedure](docs/shuttle-qa.md).
See [orders catalog and acceptance](docs/shuttle-orders-catalog.md),
[complete-price calculation](docs/shuttle-orders-quote.md),
[payment expectations](docs/shuttle-payment-expectations.md) and
[incoming pair expectations](docs/shuttle-pair-expectations.md).
See [embedded Mini App account sessions and acceptance limits](docs/shuttle-miniapp.md).
See [owner registration and payment status](docs/shuttle-account-status.md).
See [people search, directory visibility and forwarded messages](docs/shuttle-people.md).
See [massage schedules and candidate starts](docs/shuttle-massage-schedules.md).
See [partner contact-code links](docs/shuttle-partner-entry.md).
See [private PostgreSQL pool metrics and acceptance](docs/shuttle-pool-metrics.md).
See [unresolved-task metrics, freshness and failure behavior](docs/shuttle-task-metrics.md).
See [unfinished outbox metrics and native acceptance](docs/shuttle-outbox-metrics.md).
See [identity cache counter semantics and verification](docs/shuttle-identity-metrics.md).
See [River due-job backlog, timing and verification](docs/shuttle-queue-metrics.md).
See [file access](docs/shuttle-files.md), [schema compatibility](docs/shuttle-schema.md)
and the [roadmap](docs/shuttle-roadmap.md).

See [personal messages, broadcasts and delivery limits](docs/shuttle-communications.md).

## Conversation recovery

Text recovery integrates with [Loom PR #69](https://github.com/complynx/loom/pull/69),
verified at `f220e1b0821c756a3bd7b5216eb8825977cd145c`.
It activates only when Loom advertises `conversation_recovery.version: 1`.
Apply Loom's migrations and stop older Loom workers before enabling that capability.
Servers without it retain the existing run/input path. Existing saved create/input
requests keep their original endpoint, body and idempotency key during upgrades.

For an existing default chat, Shuttle checkpoints the exact text and a stable
task key before calling the durable message endpoint. Loom continues or replaces
the run in that same chat; history and operation receipts stay on the server.
Shuttle retains blocked messages in source order, fences superseded local
observers, and preserves the original session receipt for reauthorization.
An untyped proxy 404 does not close the task. Recovery retries stop making
upstream calls after two minutes; another incoming message opens a new retry
window for the saved earlier message first. Funds, authorization and uncertain
effects have separate user-visible statuses. No blocked message is automatically
abandoned, and no observer starts a paid successor without a customer message.

Loom enforces the owner's shared wallet. Its contract gives a successor the current
configured per-run allowance; it does not carry a narrower original run cap.
Shuttle never adds funds or changes those policies. Version 1 accepts text only;
Shuttle file messages already use text with durable `zinusya_files.fetch` references,
so their captions and file metadata use the same inbox. Binary file access retains
its existing authorization checks. Native commands and protocol callbacks retain
their existing handling; callbacks wait behind earlier undelivered conversation text.
No cross-chat import is used. Uncertain
effects and unavailable recovery receipts still require operator reconciliation.
