# Private conversation history

The bot supplies the latest `history.recent` events (default 12, range 1–30;
`ZNS_HISTORY__RECENT`) and a semantic summary of earlier conversation. This is
owner-private context, never authorization evidence. Current API permissions,
versions, prices and deadlines remain authoritative.

Migration 031 starts a new archive; it does not fabricate earlier message text.
The archive stores user/assistant text, command/button names and sanitized
committed domain changes. Transactional triggers capture workflow, order,
profile, knowledge, pass and massage changes, including changes outside Telegram.
Events do not invent whether an unspecified domain operation was manual or agent
initiated. Trigger payloads contain only the affected owner's state metadata,
not partner identity, passport values, payment details or knowledge contents.

Ordinary text is retained up to 16 MiB per event. The event projection is bounded
to 5000 UTF-8 bytes without splitting a Unicode character; longer text has an
owner-private body and `has_full_text` marker. The body and event are committed
atomically. Privacy checks inspect the full source before projection. Explicit
identity submissions and common
credential labels are replaced by omission markers. Profile submissions and
responses, and media uploads/requests/responses, do not copy private field values,
files or expiring transcripts into durable history. This sanitizer is conservative
pattern matching, not a guarantee of detecting every unlabeled secret. Model
snippets may be shorter; every omission is marked. Existing operational audit and
execution caches have their own policies and are not expanded by this archive.

## Summary contract

A separate model call merges the previous summary with a bounded batch of complete
sanitized events. It creates a real semantic summary, not a truncated prefix.
Input is at most 32 KiB, output at most 2 KiB, and the call has a 10-second deadline.
At most one attempt is reserved per owner/update before I/O. A failure leaves an
explicit gap and does not prevent the current request; cancellation still stops
work. OpenAI, local Codex and the authenticated remote provider support this call.

The host commits the exact batch IDs and summary using an owner-bound version CAS.
Per-event coverage handles transactions which commit a lower sequence ID late.
`through_id` is therefore only a high-water mark; `gap` reports uncovered older
events even below that mark. Summary writes are not exposed as user or agent tools.
Summary text and historical claims are untrusted data, including quoted commands.

## Reads

Authenticated GET `/v1/me/history/context?count=12` returns recent events, summary
and gap. Long bodies remain an explicit summary gap; their projections are not
treated as complete summary coverage. GET `/v1/me/history?before=0&after=0&limit=20` returns only the same actor's
archive, newest first, with `more` and `next_before`. No query can select another
owner. Context and history endpoints are read-only.

The on-demand history skill can propose `history_action:{"before":ID}`. The host
reserves at most two reads per update durably before I/O; interrupted reads count.
Each agent read contains at most four events and 6 KiB. Results and remaining
budget survive retries. Follow `next_before`; omitted content is not evidence of
absence. Normal action exclusivity and current domain authorization still apply.

Tests cover owner separation, sensitive omission, committed audit events, late
commit coverage, summary CAS, semantic summary calls, failure gaps and replayed
read budgets. Real PostgreSQL tests use disposable integration databases.

GET `/v1/me/history/{id}/text?offset=0&limit=4000&digest=...` reads at most
4000 Unicode characters from an owned event. Follow `next_offset` and retain the
returned digest; changed content returns `history_stale`. `history.read` exposes
bounded continuation to Sobek. IDs and cursors do not grant access to other owners.

Deletion keeps event identity, chronology and counts, but removes the body and
invalidates derived history through an owner generation. Cached reads, held model
results and summaries must not reintroduce deleted text. Replaying an old input
cannot recreate its body. Operational logs and execution caches keep their
separate retention policies.
Assistant replies derived from saved model plans carry the host's expected owner
generation to the internal archive boundary. Append locks the same owner summary
row as deletion, verifies the generation in that transaction, then inserts the
event and body before releasing the lock. A mismatch returns `history_stale`.
The bot archives before saving a renderable reply and persists a terminal plan
on rejection, so inbox retries and restarts cannot publish the stale response.
Manual control and system notification archives do not require a model generation.

A committed model reply remains bound to the generation in its saved host plan.
Each workflow, orders, profile, knowledge and registration reply reader validates
that generation when the reply is selected, including later explicit renders and
fresh host instances. A stale reply is omitted; its renderable interaction rows
are removed in the same transaction that stores its terminal plan marker. Effect
receipts remain intact. Manual replies without model plans and fixed host system
notices are retained. Telegram network completion is outside database transactions.
