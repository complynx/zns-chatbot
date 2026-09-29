# Forward migration tool

`tools/migrate` is a removable, separate Go module. Commands verify an
operator-prepared snapshot, create an immutable staging bundle, and plan, apply
and reconcile supported users, events, orders, passes, food and massage data.
Each domain has its own reviewed-input contract and acceptance scope.
The bounded `apply users` command writes explicitly resolved user/profile/identity
links atomically per user; see [the operator contract](go-migration.md#stage-4--production-identity-knowledge-and-removable-import).
The separate [identity preparation command](identity-import.md) uses the retained
provisioning service to prepare resumable provider identities and a complete
reviewed mapping before apply. Unlike offline planning, it contacts the configured
provider. These commands do not connect to MongoDB, download Telegram files, or
authorize a production cutover. Implementation does not imply full migration acceptance.

Use only synthetic data in the product sandbox. Real snapshots contain personal
data and must stay in an operator-controlled private archive. Reports contain
counts, digests, and fixed error codes; source IDs, paths, receipt references,
names, and document payloads are not printed. No report proves that an operator
exported every production source.

## Commands

From `tools/migrate`, using Go 1.27:

```sh
go run ./cmd/zns-migrate verify --snapshot ./testdata/synthetic
go run ./cmd/zns-migrate verify --snapshot /private/snapshot
go run ./cmd/zns-migrate stage --snapshot /private/snapshot --out /private/stages/run-1
go run ./cmd/zns-migrate plan users --stage /private/stages/run-1 --out /private/plans/users.jsonl
go test ./...
go vet ./...
../../platform/tools.local/golangci-lint.exe run --config ../../platform/.golangci.yml ./...
```

Commands write one JSON result to stdout and return nonzero on failure. Redirect
stdout to an operator-selected report outside the snapshot/stage if needed.
`verify` does not write. `stage` requires an existing parent directory and creates
a new target. PostgreSQL apply reuses the platform's pinned pgx driver. Tests reuse
the approved testify v1.12.1 dependency and its existing YAML dependency; neither
is linked into the CLI.

## Snapshot contract, version 1

The root contains `manifest.json` and only the files declared by that manifest.
Manifest JSON rejects unknown fields, case aliases, duplicate keys, invalid UTF-8,
trailing data, and nesting deeper than 64 levels. Paths use relative slash-separated
ASCII components. Absolute paths, traversal, backslashes, alternate data streams,
reserved Windows devices, symlinks, and Windows reparse points are rejected.
Every selected-root ancestor must also be a physical directory. `os.Root` confines
subsequent file access even if a child path changes after the initial check.
This is a private offline archive, not a shared directory for concurrent writers.

The manifest has these fields:

| Field         | Meaning                                                                        |
| ------------- | ------------------------------------------------------------------------------ |
| `version`     | Integer `1`.                                                                   |
| `snapshot_id` | Operator-chosen stable ASCII token, at most 128 characters.                    |
| `bot_id`      | Positive legacy Telegram bot ID. Bot filtering is a later conversion gate.     |
| `captured_at` | RFC3339 timestamp with an explicit offset.                                     |
| `consistency` | `stopped_writer` or `consistent_snapshot`, declared by the exporting operator. |
| `coverage`    | Explicit inventory described below.                                            |
| `files`       | Declared files with exact byte counts and SHA-256 digests.                     |
| `proofs`      | Private references to receipt bytes or explicit unavailability.                |

Coverage must name each logical domain exactly once: `users`, `events`, `passes`,
`orders`, `order_capacity`, `massage`, `messages`, `files`, `bot_storage`,
`knowledge`, `schedule`, and `configuration`. Each entry contains `domain`,
`name` (actual configured collection/resource name), `status`, and `reason`.

- `included`: at least one file must exist, including an empty JSONL file for a
  source verified to contain zero records.
- `absent`: a nonempty reason is required. The report records an absence that
  needs conversion/cutover review; it does not silently infer that the source is
  irrelevant.
- `unavailable`: a nonempty reason is required and verification fails.

Current Python default collection names are `zns_bot_users`, `zns_bot_events`,
`zns_bot_passes`, `zns_bot_food`, `zns_bot_food_capacity`, `zns_bot_massage`,
`zns_bot_messages`, `zns_bot_files`, and `bots_storage`. Actual deployment
configuration is authoritative. Knowledge and schedule also use resources such
as `static/rag_data.yaml` and `static/line-up.csv`. Export secret-free effective
business configuration separately; do not archive API keys as business settings.
Any additional deployed source requires an explicit contract update and review.

Each file entry contains `path`, `source` (logical domain), `kind`, `sha256`
(lowercase hex), `bytes`, and `records`. Kinds are:

- `records`: UTF-8 JSONL with one complete object per line, no blank lines. Each
  object must have `_id`. IDs support strings, signed 64-bit integer JSON values,
  and canonical Extended JSON `$oid`, `$numberLong`, or `$numberInt` forms.
  Equivalent integer forms collide. Duplicate IDs across shards of one source
  fail. Unsupported IDs require an explicit converter/export contract change.
- `blob`: nonempty immutable receipt bytes, referenced by `proofs`.
- `resource`: opaque non-record files; their contents are preserved and checksummed,
  not interpreted. Use this for business configuration, knowledge, schedules, or
  other archived assets awaiting a specific converter.

Records retain **all** fields, including unknown fields and Extended JSON values.
Staging copies original files byte for byte; it does not reserialize, normalize,
truncate, redact, or drop record fields. Canonical ID hashes are used only for
duplicate detection, never to replace archived source data.

Each proof contains `source`, `record_id`, `owner_id` (legacy users document `_id`),
`field`, `telegram_file_id`, `chat_id`, `message_id`, `blob`, and `unavailable`.
The source and owner records must exist. An available proof names a declared
`blob`; a missing proof sets `unavailable: true` and leaves `blob` empty. Every
receipt blob must be referenced. Missing bytes are counted in the report and
remain a conversion blocker. Empty files cannot stand in for receipts.

These references are an operator inventory, not proof that every legacy payment
field was found or that a receipt belongs to its claimed owner. The domain
converter must establish that linkage. Legacy `proof_file: "cash"` is a payment
state sentinel, not a Telegram file to download. A validated legacy payment must
not be relabeled as a newly verified receipt by this tool.

## Bounds

All counts and sizes are checked before and during reads. Flags configure the
following defaults; increasing a limit is an explicit operator choice.

| Flag                 |   Default | Maximum accepted setting |
| -------------------- | --------: | -----------------------: |
| `--max-files`        |       256 |                   10,000 |
| `--max-records`      | 1,000,000 |               10,000,000 |
| `--max-total-bytes`  |     2 GiB |                    1 TiB |
| `--max-file-bytes`   |   512 MiB |         total-byte limit |
| `--max-blob-bytes`   |    20 MiB |           per-file limit |
| `--max-record-bytes` |     1 MiB |                   16 MiB |

All values must be positive. The manifest itself is at most 1 MiB, regardless of
flags. Proof entries are bounded by the file limit. Paths have at most 16
components and 1,024 characters; individual components have at most 128
characters. The retained duplicate-ID index is bounded by the record limit.
Size flags use integer bytes, not human-readable suffixes.

## Staging and replay

The stage bundle contains `snapshot/` (the exact manifest and declared source
files) and `stage.json` (the sanitized verification report). It copies into a new
private temporary sibling, syncs written files, verifies copied checksums and
records, then publishes by directory rename. It never merges into another
directory or modifies the source. The output cannot overlap the source tree.

An exclusive sibling lock prevents concurrent CLI writers for the same target.
An exact replay re-verifies both source and staged bytes and the receipt before
returning `reused: true`. Changed content, manifests, receipts, or extra files
fail; the tool does not repair or overwrite them. Immutability is an application
contract checked on replay, not a filesystem ACL or protection from an operator
who can rewrite the whole archive.

Handled errors remove the unpublished temporary directory. A crash may leave a
temporary directory or lock; inspect it and remove only that task's artifacts
before retrying. There is no automatic stale-lock deletion. The tool syncs files
but does not promise directory-entry durability across sudden power loss.
Rollback at this stage is discarding a separately verified staging bundle; no
business database has been changed.

`verified: true` means declared file integrity passed. `import_ready` is always
false in this scaffold. Fixed report gaps retain unimplemented domain conversion,
identity provisioning, independently proven source coverage, and receipt inventory
and ownership checks. An incomplete receipt archive may be staged for forensic
preservation, but it cannot be described as ready for payment import.

## Offline users/profile plan

`plan users` verifies the complete staged snapshot and its receipt before reading
user records. The included users source must contain JSONL `records` files. It
writes a private JSONL artifact outside the stage, through a
temporary file and exclusive target lock. It preserves the original stage. An
exact replay returns the same artifact digest; a changed existing output is never
overwritten. The artifact is capped at 256 MiB. The same `--max-*` input-limit
flags apply. The output parent must already exist.

The artifact contains a versioned header tied to the exact manifest digest, one
entry for each source users record, and a summary. Each entry retains its Mongo
`_id`, source collection/file/line, record checksum and stable legacy key. The key
is scoped by selected bot, collection and canonical Mongo ID; it is not a Core
owner ID or a Zitadel subject. No user identity provisioning is attempted.

The stdout result contains counts, digests, `reused`, and `apply_ready: false`.
It never contains user fields. The JSONL artifact **does contain private profile
values**, including passports when supplied. Its file mode is owner-only on Unix;
the operator must provide a private Windows directory with appropriate ACLs.
Do not publish or copy the artifact into the product sandbox.

| Legacy users field                                   | Plan behavior                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `bot_id`                                             | Exact integer match to the manifest bot. Valid records for another bot are explicitly excluded and produce no profile candidate. Missing, malformed or out-of-range bot IDs produce blocked records.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `user_id`                                            | Positive Telegram identity below the target schema limit, distinct from Mongo `_id`. Integer Extended JSON forms are accepted; quoted numeric strings are not guessed. Two selected records with the same Telegram ID abort publication.                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `role`                                               | Exact `leader`, `follower`, or empty. This is a dance role, never an administrator role. Other values block the candidate.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `legal_name`                                         | Exact string, at most 300 Unicode characters, without control characters. Missing means the target's empty profile value. No display-name inference, splitting or normalization. Null/invalid values remain unresolved.                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `passport_number`                                    | Same validation; proposed `passport` value. No invented passport or normalization.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `legal_name_frozen`                                  | **Presence means frozen**, including a source value of `false` or `null`, matching the Python reader. Absence means unfrozen.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `language_code`                                      | Preserve the original string up to 64 characters. Missing/null proposes empty language. Tested EN/RU and `be`/`by`/`uk`/`ua` aliases map to current presentation fallback; PL/DE map to EN. Supported regional examples are `en-US`, `en-GB`, `ru-RU`, `be-BY`, `by-BY`, `uk-UA`, `ua-UA`, `pl-PL`, `de-DE`, case-insensitive with underscores accepted. Other tags remain unchanged with `locale_mapping_required`; no approximate BCP47 parser is substituted.                                                                                                                                                                                                           |
| `print_name`                                         | Exact `print_name` and display candidate (maximum 513 Unicode characters), never legal identity. No derived print name or fake name splitting. `first_name`/`last_name` preserve exact strings up to 256 Unicode characters; `username` allows at most 64 ASCII letters, digits or underscores. Missing metadata maps to empty with `default_absent`; explicit null is accepted only for username/last_name with `default_null`. Invalid types, controls, noncharacters U+FFFE/U+FFFF and replacement characters stay blocked. Invalid JSON UTF-8 is rejected during snapshot verification. Import leaves `telegram_metadata_update=-1`. Raw source remains authoritative. |
| `banned`                                             | Preserve presence as `legacy_banned`, including false/null. Add `ban_policy_unmapped`; do not translate it into a guessed `can_book` grant or silently reactivate the user.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `massage_specialist.notify_bookings`, `.notify_next` | Preserve actual booleans; absent fields default to true only inside an actual specialist object, matching Python. Other value types block. Event/specialist linkage remains unresolved, so these are not global user notification settings.                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `state`                                              | Only exactly `{"state":""}` is classified as archived idle state. Every active/other shape adds `active_state_unmapped`. Pending forms and deadlines are not recreated or silently discarded.                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| All other top-level fields and specialist subfields  | Explicit private field dispositions and blockers. This includes provenance/statistics/plugin fields, localized display names, payment-admin defaults, avatar data and arbitrary role/grant-looking fields. Original values remain intact in the staged archive.                                                                                                                                                                                                                                                                                                                                                                                                            |

Every candidate keeps `target_owner`, `zitadel_issuer`, `zitadel_subject`, and
`can_book` null, with identity and eligibility-policy blockers. Recognition of a
field is not permission to apply it: invalid values remain null and block, while
known-but-unmapped fields retain explicit dispositions. Unknown fields are never
silently dropped. Other-bot and unscoped records remain represented by legacy
references and exclusion/block reasons; their private profiles are not copied
into this bot's candidates.

The users planning command has no database connection. Bounded `apply users`
requires the separately attested resolutions described in the operator contract;
it cannot resolve other data blockers or provision identities.

## Pass-event catalog and configuration

The current event plan version 2 and pass plan version 4 expand the earlier
foundation through migration058. The complete source contract and announcement
delivery policy are in [event-parity-contract.md](event-parity-contract.md).
Independent acceptance of the frozen foundation does not accept this expansion.

`plan events --stage PATH --out PATH` produces a private JSON artifact for the
included events collection. `apply events --stage PATH --plan PATH --resolutions
PATH` connects using only `MIGRATE_DATABASE_URL`, after regenerating identical
plan bytes from the immutable stage and checking every resolution. Event records
are global within the source deployment (Python `Events.refresh` reads the whole
events collection); manifest bot identity scopes durable source keys. No guessed
per-record bot filter is applied. An operator must attest that the snapshot is the
effective configuration for that deployment.

The resolution object requires `version:1`, `plan_sha256`, `dates_verified:true`,
`configuration_verified:true`, `admin_grants_verified:true`, and `events` containing
exactly one `{legacy_key,display_order}` per source record. Display positions must
be unique nonnegative int32 values, reviewed against Python sale-start ordering.
Unknown/duplicate JSON keys, missing attestations and changed artifact bytes fail
closed. Attestations do not bypass conversion blockers or add administrators.

Source field shapes: `title_long` and `title_short` each accept a string, a
locale-to-string object, or null; this foundation requires equivalent effective
titles. `payment_admin` and `hidden_payment_admins` each accept an integer Telegram
ID, an array of integer Telegram IDs, or null. Visible and hidden sets must not
overlap. Each `pass_types` entry has `amount` (nonnegative integer, default 0),
`price` (positive integer), `start` (explicit RFC3339 instant), `promo` and
`blocked_by_date` (booleans, default false). The source event name is `key`, not
`title` or `id`; Mongo attribution remains in `_id`.

Supported fields map to pass events, ordered tiers and payment administrators:
`key`, optional `finish_date`, `require_passport`, `pass_assignment_rule`,
`disable_max_concurrent_assignments`, separate long/short titles, `pass_types`,
`payment_admin`, `hidden_payment_admins`, `country_emoji`, `thread_channel`,
`thread_id`, `thread_locale`, `price` and `amount_cap_per_role`. Explicit instants must be RFC3339 strings
or `{"$date":"RFC3339"}` with at most microsecond precision; naive explicit dates and
numeric Extended JSON dates remain blocked pending reviewed timezone conversion.
An absent/null event finish has an explicit open-ended marker and compatibility
sentinel; an absent tier start uses the source datetime.max sentinel.
The date attestation confirms the source clock semantics, not just syntax. Defaults
follow `zns-chatbot/events.py`: distributed assignment, false flags, tier amount0,
empty admin lists. Invalid explicit types never become defaults. Target numeric
limits are enforced before SQL.

Localized titles preserve normalized locale keys and trimmed strings. The planner
materializes EN/RU using Python's exact/regional/en/ru/default fallback order and
the first normalized source entry when those keys are absent. Long/short titles
are separate. Empty tiers and event fallback pricing are supported. Role capacity
is preserved as source metadata; the Python runtime has no active consumer, so
no new allocator cap is imposed. Every unknown field remains blocked, and the
original archive preserves the source spelling and bytes.

Each event, tiers, administrator links, durable migration045 source reference and
temporary receipt commits atomically. Administrator Telegram IDs must resolve to
already imported same-bot Core identities; no user creation, email matching or
global booking/admin grants occurs. Visible/hidden overlap or duplicate admins is
blocked. Receipts bind plan/resolution bytes and the initial administrator owners.
Replay compares all imported fields and complete tier/admin row counts; changed
target values, changed identity ownership and existing event conflicts fail
without repair or overwrite. Earlier event commits survive later conflicts and
resume safely. Stop runtime writers during import and per-record reconciliation.
Core references outlive removable `migrate_import.event_receipts`. CLI diagnostics
contain only counts/hashes/stable errors. Only synthetic acceptance is authorized.

## Domain mapping work still required

This matrix defines conversion ownership and blocking checks. The CLI implements
users, events, orders, passes, food and massage planning/apply paths with separate
acceptance scopes. Messages and complete retained-resource reconciliation remain
open. Every active field must
receive a mapped target or an explicit reviewed
archive-only disposition before apply exists. Do not infer defaults from absent
legacy fields without tracing the active Python reader.

| Source                                                 | Intended target boundary                                                                  | Required checks before conversion                                                                                                                                                                                                                             |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Users and bot-scoped identity fields                   | `core.users`, Telegram/Zitadel identity links, language preferences, `core.pass_profiles` | Filter bot deployment; distinguish Mongo `_id` from `user_id`; provision durable identity mapping; retain legal name, passport, role and notification preferences without granting roles from user text.                                                      |
| Events and effective business configuration            | `core.pass_events`, tiers/titles/admin tables; separate order/massage events              | Preserve finish dates, deadlines, localized titles, role capacity, assignment rule, tier order/start/promo/date blocks and visible/hidden payment admins. An order deadline is not an event finish. Explicit admin rights require source evidence.            |
| Orders (`food_collection`) and capacity collection     | `core.orders`, event menu/extras/capacity, payment attempts/proofs                        | Preserve all choice fields, state/deletion/validation/cash distinctions, timestamps, attempt token, country/admin, reservations and reminders. Reconcile limited-service capacity using the active Python semantics; do not rerun allocation while importing. |
| Passes and coupled registrations                       | `core.pass_bookings`, payment attempts, admin rights                                      | Preserve waiting invitations, waitlist/assignment order, role/type/couple symmetry, per-person and aggregate prices, tier/assignment timestamps, skip-balance and proof/admin state. Asymmetric or dangling couples block conversion.                         |
| Receipt references and archived bytes                  | Owner-bound Core proof IDs before linked payment records                                  | Resolve Telegram references outside this offline tool, checksum nonempty bytes, prove source record/owner linkage, preserve original refs in archive, report missing files, and distinguish imported validation from a new review.                            |
| Massage records and specialist configuration in users  | Massage events/parties/specialists/work/booking tables                                    | Preserve slot length, timezone/event mapping, original prices/currencies, cancellation/instant bookings, specialist capability and notification-delivery flags. Reconcile overlaps and table capacity without silently dropping conflicts.                    |
| Assistant messages, files and user-private context     | Conversation and private knowledge boundaries                                             | Explicit privacy/retention policy; source owner/bot attribution; do not copy expired media/transcriptions into durable conversation. No legacy message becomes authority, a role grant, or an approved shared fact.                                           |
| Knowledge documents and schedule resources             | General/event facts and schedule representation                                           | Capture external-document snapshots and provenance; separate historical/current event scope; preserve language and effective times; require curation policy. Do not claim the present Go runtime has every old refresh/schedule function.                     |
| Bot storage variables and remaining user/plugin fields | Reviewed configuration, target fields, or source archive                                  | Inventory every key and active reader, including avatar/auth/superuser data. Secrets are not business data. Unknown active fields block final mapping approval; archival preservation alone is not feature parity.                                            |

Assistant-history retention was approved on 2026-09-27: preserve the full available
period of personal correspondence with the assistant. This source excludes other
bot actions; do not manufacture a combined activity log. See the
[approved history contract](messages-migration-contract.md).

The existing domain converters and identity preparation have scoped evidence;
they do not establish whole-archive acceptance. Remaining work includes the
[assistant history contract](messages-migration-contract.md), complete key/reader
dispositions, and nonempty knowledge/schedule resource binding evidence. The Go
runtime already has static QA, external about-source refresh and lineup readers;
their configured artifacts must remain usable after removal of staging. Static
QA documents must not be reclassified as approved shared facts during import.
Repeat dependency-ordered apply, reconciliation and removal on the final shared
composition with fresh Code QA and Functional QA. Production cutover requires
separate authorization.
No reverse migration is planned. Newer production writes must never be overwritten
by an old snapshot. Remove the tool, temporary schemas/credentials and importer CI
only after all parity and cutover gates pass; retain runtime identity links, legacy
references, source archive and normal SQL migration history.
