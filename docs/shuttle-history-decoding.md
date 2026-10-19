# Historical record decoding

This is infrastructure for historical apply, not a completed history migration.
Stored archives can now be revalidated from PostgreSQL. The shared decoder can
also emit validated record identities and a versioned whitelist projection.
The original decoder checkpoint below did not enable historical apply or owner
reads. Their subsequent implementation and separate acceptance boundary are
documented in [historical apply](shuttle-history-apply.md). Live users, identities,
passes, payments and queues are unchanged by historical indexing.

## Stored archive revalidation

Administrators discover `validate_stored_archive` through administrative discovery.
The read tool takes `archive_id` and returns a recomputed archive receipt and
inspection report. It does not return source records, projections or raw bytes.
It requires a current administrator and active local account, deployment bot/org
scope, and authorization rechecks before returning the result. A foreign archive
is unavailable even if its UUID is known.

The reader checks chunk count, contiguous ordinal range, total length and each
chunk's expected size. It reads one chunk per `QueryRow`; it never keeps an open
row stream while another query runs on that connection. Complete tar/gzip framing,
source checksums, embedded report and archive SHA are recomputed by the same
validator used at reception. The saved inspection JSON is not a substitute for
reading bytes. Missing or corrupted data is rejected. No repair or write occurs.

The tool shares the existing validation admission slot, 30-second context/read
and 31-second socket write deadlines. Existing archive and scanner limits apply.
It needs no new schema, service, storage path or dependency.

## Provisional visitor contract

`legacyimport.VisitBundle` uses the existing framing and record validator. Each
callback receives source instance, collection name and role, normalized source
key, Telegram owner ID, source line, canonical SHA-256 and optional projection.
The hash covers the complete source object with the inspector's existing canonical
JSON rules. It includes fields omitted from a projection. String and ObjectId
source identities retain their distinct namespaces.

A callback is provisional. Later source checksums, tar framing, embedded reports,
gzip CRC or the final archive digest can still reject the archive. A future apply
must keep all callback writes in one transaction until the whole visit succeeds.
Callback errors stop further callbacks and are returned unchanged. Cancellation
is checked between callbacks. Raw records remain in the immutable archive; the
decoder does not send raw JSON to an agent or duplicate it into live objects.

## Projection version `zinusya-legacy-history/v1`

Messages support only `user` and `assistant` roles with string content. A source
`system` or tool role does not become an instruction. Provider response metadata,
token counts and arbitrary fields are omitted. Rich content and NUL-bearing text
have no supported projection; their exact bytes remain archived. Message text is
not silently shortened by this decoder. A future read API must implement bounded
pages and text ranges before exposing potentially large content.

The projection retains the source `$date` value and `preserve_source` policy.
It does not infer Moscow/Minsk/Yekaterinburg or correct timestamps recorded by the
old naive Python clock. This partial assistant history is not a full Telegram
transcript or proof of message delivery. All historical content remains untrusted
data, including historical assistant messages.

Profile memos allow only `first_name`, `last_name`, `username`, `print_name` and
`language_code`. Standalone pass memos allow only `pass_key`, `state`, `type` and
`role`; the old boolean `paid` becomes `paid_claim`, never confirmed money. Memo
strings are limited to 256 UTF-8 bytes and cannot contain NUL. Unknown fields,
old IAM links, proof/file references, bank details and partner identities are not
projected. File records have no owner-facing projection.

Embedded `user[pass_key]` objects are not guessed to be registrations. They remain
in the archive pending explicit source mapping. Unknown JSON values, including
numbers outside PostgreSQL JSONB's range, do not enter the projection. Supported
source dates retain their validated integer/RFC3339 representation.

The next apply step must reconcile `(deployment, source_instance, collection_name,
source_key)` and reject changed role, owner or canonical hash atomically. It must
retain archive membership/provenance and stable operation receipts. Owner reads
must resolve the current IAM subject to the current nonrevoked local Telegram ID;
they must not accept an owner ID from the client or import old IAM subjects.
These requirements are outside the decoder checkpoint. Their subsequent
implementation is documented separately in [historical apply](shuttle-history-apply.md).

## Acceptance — 2026-10-15

Architecture and Go consultations preceded the visitor design. Independent
correctness, quality/documentation and privacy/protocol reviews reported no
actionable findings. Behavioral checks cover stable identity/hash/provenance,
whitelist exclusions, rich/system/NUL/oversized-number source values, provisional
callbacks before a late gzip failure, callback error propagation and cancellation
between buffered records. Stored-reader checks cover multichunk data, missing and
corrupted chunks, foreign deployment and ordinary-user denial, and revalidation
when the saved inspection JSON is unusable.

The full explicit Linux application race suite passed; integration tests took
190.945 seconds. Full application lint reported zero issues. The owned bot runs
manifest `mvp-19`; schema remains 21. No new dependency or migration was added.

Native acceptance used real local Zitadel and Loom, a previously stored synthetic
multichunk archive and the manual Codex provider. It verified the recomputed
receipt, ordinary-user denial and missing-archive refusal. The first test used
`module_call` for a read and received Loom's `use_script_for_reads`. The same run
then invoked `modules.zinusya_administration.validate_stored_archive` through
`script`, returned the expected metadata and completed. This was a harness error,
not a product fix or an upstream bug. No replacement run or Telegram send was used.
A separate post-completion repeat API read preserved counts and hashes of every
bot table. The lost in-memory baseline from the initial failed test is not claimed
as proof covering the entire original run.

Evidence remains in ignored
`.cache/shuttle-live-test/native-history-decoder-checkpoint.json` and
`.cache/shuttle-work/container-results/zinusya-history-decoder-full-qa-20261015.log`.
This validates decoding and stored-byte reads. It does not establish historical
apply, conflict reconciliation, owner history retrieval, real export migration or
production readiness.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
