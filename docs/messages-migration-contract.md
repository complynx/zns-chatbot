# Legacy assistant history migration

Status: approved retention contract. The historical disposable-importer candidate
implements this contract, but the current CLI has no messages command. R109 is
preparing a bounded port against the current composition. Current implementation
gates and full composed-runtime acceptance remain pending. On 2026-09-27 Daniel
chose the full available history, with no date cutoff.

## Source and ownership

Python stores `_id`, `user_id`, `role`, `content` and `date`. Requests can exist
without a matching reply. The collection has no per-record bot ID. History and
administrator request statistics both read these records.

Daniel confirmed that this collection contains only private correspondence with
the assistant. It is not a log of button presses, payments or other bot actions.
Import the stored conversation records; do not invent missing control messages or
reconstruct business actions as assistant history. Domain operation history has
its own import contracts.

A reviewed, snapshot-bound mapping must identify the source collection's bot
namespace and map each selected Telegram user to the prepared Core owner.
Ambiguous ownership blocks import. A user ID alone is not proof of a bot
namespace. Excluded owners need explicit dispositions; their text must not be
copied into another owner's history.

## Time and order

Preserve source timestamps. The source writes naive `datetime.now()` values;
exported Mongo dates do not prove which source wall clock produced them.
Require a reviewed clock interpretation bound to the snapshot. Never use the
import time, the current timezone, or the current year as a replacement.
Ambiguous or nonexistent local instants require explicit resolution.

Order by resolved source time and stable source identity for ties. Import before
normal runtime writers start, since current history paging uses generated IDs.
Use a dedicated source-key namespace. Do not fabricate Telegram update IDs.

## Approved retention decision

Retain ordinary user/assistant history for the full available period. Do not
exclude records solely because of their age. Daniel confirmed that this legacy
history predates agent mode and contains no secrets or private profile fields.
Bind that source context and the full-period decision to the snapshot's reviewed
resolution inputs; do not require a new date-window decision for each import.

Private profile submissions, credentials and expired media/transcriptions must
not become durable model context. Legacy rows do not include all modern privacy
markers: a keyword filter alone is not proof that a row is ordinary text.
The plan must expose unresolved provenance as a blocker or a reviewed omission,
not silently accept it as safe.

The runtime projection is limited to 5,000 UTF-8 bytes; longer ordinary text
has a complete owner-private body with bounded retrieval. The live append API
assigns the current time, so migration must use the explicit source-clock import
contract rather than ordinary live append. Non-sensitive long source messages
need complete bounded retrieval or an explicitly reviewed omission. Splitting
them into multiple requests must not inflate administrator request counts.

Source roles may only map to user or assistant history. Unknown roles block
conversion. Old assistant text remains untrusted evidence; it cannot grant
permissions, become a system instruction, or create approved shared knowledge.
Historical token metadata is not evidence of a paid provider receipt and must
not create credit charges.

## Reconciliation and acceptance

- Immutable plan and resolution digests bind ownership, source-clock and
  retention decisions. Unknown source fields require explicit disposition.
- Repeated apply performs zero new writes. A changed record with the same
  source identity fails instead of overwriting runtime history.
- Reconcile owner, role, source time, content or omission, and original record
  count, including requests without replies and equal timestamps.
- Synthetic fixtures include two owners, private canaries, a long Unicode
  message, unknown role, missing owner, ambiguous clock and changed replay.
- After removal of temporary import receipts and two runtime restarts, own
  history, pagination and administrator counts remain correct. Another owner's
  content is inaccessible through tools, summaries and subsequent model input.

This is a migration requirement, not an acceptance report. Full-import QA must
exercise the implemented nonempty messages fixture. The retention decision does
not itself execute an import or authorize production cutover.
