# Pass import preservation contract

Status: implemented; focused PostgreSQL and runtime tests pass. Independent QA is pending. No production import has run.

## Scope and source selection

The removable tool consumes the verified stopped-writer snapshot, not MongoDB or Telegram. The selected bot's `passes` source is authoritative. A user document's event-key object is fallback only when that exact event exists in the selected snapshot's event registry and no dedicated record exists for the same bot/user/event. Both records remain durable source evidence. For embedded user data, runtime provenance stores only the relevant pass object/preferences/field-presence markers plus the original whole-user digest and source locator. The complete user record stays in the private migration archive. Unknown keys are not accepted by shape alone.

A source-less registration is not created. Python cancellation deletes records: absence does not prove a cancelled booking.

## Mapping

| Source | Target and invariant |
| --- | --- |
| bot_id/user_id/pass_key | Exact accepted user/event legacy references and source digests; Telegram namespace must match. |
| state | waitlist, waiting-for-couple, assigned or paid retain their meaning. Python paid may be a pending receipt, not acceptance. |
| role/type | Role and custom kind remain unchanged. |
| couple | Pending invitation uses invitation_target; established pairs use reciprocal owners. Missing or conflicting partner data requires an explicit correction, not automatic splitting. |
| date_created/date_assignment | Preserve instants and ordering. Invitation/deadline processing resumes from these instants. |
| price/pass_type_index | Preserve integer per-person price and zero-based tier. Do not reprice against today's event tiers. |
| skip_in_balance_count/comment | Preserve nullable exclusion policy and comment. |
| proof_admin | Current booking contact, distinct from receiving/reviewing provenance. |
| notified_deadline_close/close2 | Independent assignment-keyed first/second markers. A second-only source keeps first_at NULL; subsequent first-reminder delivery preserves the second marker. |
| proof_file=free_pass | Paid/free source fact; no fabricated payment attempt or receipt bytes. |
| proof_received/proof_admin_received | Current-generation receiving timestamp and explicit actor; missing actor stays unknown. Older-generation facts remain raw/archive evidence, never a current receiver backfill. |
| proof_accepted/proof_admin_accepted | Exact current-generation accepted timestamp and actor if recorded, projected per participant. Missing actor remains unknown. |
| proof_rejected | Exact historical rejection timestamp; Python does not record rejecting actor. |
| actual proof_file | Manifest-bound receipt reference and verified bytes, or explicit unavailable evidence. Shared couple receipts retain their participant set. |
| user.proof_admins[event] | Future registration contact preference consumed by registration creation after current admin eligibility checks. |
| user.notified_passport_data_required | Global field-presence suppression marker consumed by the startup passport reminder processor. |

Stale payment fields can survive Python updates. Never attach a historical timestamp or receipt to a later assignment based solely on its presence. Preserve the full source, and require explicit conflict resolution where the current generation cannot be established.

## Required compatibility seam

Migration 055 adds the compatibility seam. Durable pass import references retain raw source, digest, identity and target. For actual receipts, legacy provenance must guard any nullable submitter/reviewer; ordinary runtime writes retain strict authenticated-actor constraints. Payment reads must represent an unknown actor explicitly. A proof's storage custodian must not be presented as its uploader. Exact participant/provenance authorization replaces uploader equality only for validated legacy receipts.

No-attempt paid/free bookings are already valid in core.pass_bookings. Preserve their historical free/received/accepted/rejected metadata in a typed legacy projection so exports do not erase it. Do not synthesize accepted attempts merely to satisfy a schema.

Unavailable receipt bytes must remain unavailable, with a truthful UI result. Never use empty or dummy bytes as a receipt. An existing reviewed payment is not downgraded because its archived file is unavailable.

## Apply and reconcile

Regenerate and compare the complete plan before apply. Bind resolutions to the plan digest. Verify user/event dependency digests and all proof inventory before writing. Stop both writers during cutover.

Apply the complete bounded pass stage in one transaction under source and sorted event locks. Insert registrations, reciprocal pairs, deadline markers, receipt attempts and participant snapshots, byte payloads, preferences, durable references and the stage receipt together. Do not allocate queues or enqueue personal notifications during import. Migration058 registration announcements follow the explicit bound historical policy described in ../../docs/event-parity-contract.md. Refuse target event booking conflicts rather than merge them.

Replay requires the same plan/resolution digest and exact target snapshot; it returns reused. Drift returns an error. Read-only reconciliation detects added/deleted/changed rows and receipt bytes. Global user markers and deferred-domain completion commit with the full stage; failed runs leave no partial event mutations.

User-stage pass deferral is mandatory work, not full completion. Reconciliation must expose unresolved deferred domains until pass application succeeds.

## Rollback

The source is untouched. Before runtime starts, restore the target backup for an unsuccessful rehearsal/cutover. Once runtime has accepted writes, do not reverse-merge or delete imported rows: stop writers and use an explicitly reviewed recovery plan. No production action is authorized by this contract.

## Proof required

Focused conversion tests: dedicated/fallback precedence, bot filtering, custom kinds, nullable balance policy, conflicting pair data, deadline markers, pending/accepted/rejected/free receipts, unknown actors, shared receipt grouping, missing blobs and conflicting historical generations.

Real PostgreSQL tests: dependency binding, event atomicity, crash/retry, exact replay, drift detection, immutable source references, authenticated nonlegacy write constraints and unknown-actor read safety. Fresh Code QA and black-box Telegram-like Functional QA are required after the implementation is complete.

## Commands

Use `zns-migrate plan passes --stage <verified-stage> --out <private-plan>`.
Apply with `zns-migrate apply passes --stage <stage> --plan <plan> --resolutions <private-resolution>` and `MIGRATE_DATABASE_URL`.
Use the same arguments with `reconcile passes` for read-only verification.
The resolution has version 1, plan_sha256, dates_verified, bot_namespace_verified and writers_stopped. Plan v4 additionally requires historical_announcements (suppress_historical or preserve_source_eligibility) when a source registration lacks sent_to_hype_thread. All attestations must be true. Plan conversion errors require correction of the reviewed source export; an edited plan is never trusted.

A shared receipt stores an existing participant as internal file custodian, with submitter explicitly null. One attempt retains its full participant set. Attempt actors are populated only when participants agree; otherwise they remain null and payment reads use each participant's typed source actor. A new authenticated review supersedes that review projection while raw evidence remains. Missing received-admin is never synthesized from the current contact. Unknown reviewer stays null. Ordinary nonlegacy payment constraints remain strict. Imported unavailable proof metadata does not create dummy receipt bytes.

Passport reminder startup failures abort startup for retry. The once-per-user marker and durable notification outbox write are atomic. Both false/null source notification fields and existing passport-number fields suppress the legacy missing-field reminder. The notice opens the existing profile card without starting a form or capturing unrelated free text.

Plan version 4 retains the generation and per-participant actor rules and binds announcement marker presence/raw values. Resolution version remains 1 with the explicit historical announcement policy.

The one-based assignment_tier_number is a separate source fact from zero-based pass_type_index. It is validated as positive, stored with its assignment generation, and exported independently; it never changes allocation tier_index. Current effective receipt decision/timestamp defines shared-receipt grouping; stale review fields remain raw evidence only.

Embedded food markers create a separate user food obligation before pass apply. Dedicated pass markers create legacy_pass_deferred_domains obligations. Pass apply completes only passes and reports pending_domains across both obligation tables. Pass reconciliation verifies food evidence without claiming ownership of its completion bit; only the food owning stage can resolve that work.

Receipt grouping requires the same event, reciprocal-pair identity (or the same solo identity), file reference, received instant, active-versus-historical applicability class, and effective decision/review instant. Participant assignment instants are preserved individually and do not split a current shared receipt. Unrelated pairs/solo users cannot merge from a reused file/time alone. Historical/current participants remain separate so an old receipt cannot become a current attachment.

Imported assignment metadata also preserves an absent/null assignment_tier_number as unknown. Export derives tier_index+1 only for native or later assignment generations without imported source metadata.

Migration058 maps sent_to_hype_thread with exact presence semantics: even present null/false suppresses the announcement. Missing markers require explicit historical policy; immutable source metadata is reconciled independently of mutable delivery records. See ../../docs/event-parity-contract.md. Unsupported notified_no_more_passes remains blocked because no active source reader/disposition is established. Frozen candidate17 does not include these v4 changes. Fresh Code QA and Functional QA of the complete pass preservation contract plus 058 are required.

