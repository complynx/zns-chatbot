# Agent event configuration QA, 2026-10-15

This bounded slice is accepted on the owned stand with schema 31 / manifest
`mvp-30`. Source, native recovery and scoped preservation checks passed, subject
to the explicit limits below.
No production deployment or cutover is approved by this record.

## Source verification

The store and HTTP tests cover effect-free preparation, normalized create and
replace, optional-policy removal, immutable replay after a later edit/restart,
current role and local-revocation denial, scoped actor/proposal isolation,
operator-receipt collisions, concurrent confirmation/create/CAS, database expiry
after update and insert waits, and atomic rollback of event, policies and result.
An assigned pass retains its frozen terms and payment rows after configuration
replacement. A PostgreSQL protocol proxy drops the COMMIT reply: the API returns
503 and same-operation recovery returns the original receipt without another
mutation. This fault injection is source integration QA, not a native Loom fault.

Actual raw HTTP tests cover strict unknown-field rejection, absolute timestamps,
null policy removal, optional false tier flags and full 100-module/100-tier
configuration. The confirmation measures 32,089 bytes. After the description
fix, preparation measures 33,349 bytes. Maximum multibyte/control-character names
keep the description within Loom's 4,000-byte limit without removing arguments.

Telegram HTTP-peer/River tests show the entire JSON literally across UTF-16-sized
chunks, with buttons only on the last message. Delivered legacy summary chunks
do not suppress the new presentation; old callbacks fail and the new callback
reaches Loom once. Restarted approval controls known not to have been applied
are revalidated; unknown outcomes preserve immutable idempotent reconciliation.
Argument-free approvals retain legacy callback keys. Those source tests alone do not establish actual Telegram display. A subsequent
[separate real-client fixture check](shuttle-approval-display-qa-2026-10-15.md)
now covers complete visible parameters and rejection; its RPC leaf is synthetic,
so it does not extend this event-write scenario to a Telegram administrator.

Initial store and full feature correctness/quality reviews passed after fixing
optional tier flags. A separate proposed null-policy defect was disproved by
raw requests; no nullability tags changed. Fresh description/presentation reviews
found a persisted known-unapplied control bypass, which was reproduced in the
recovery path and fixed. Both final correctness/quality reviews report no
remaining actionable findings. Native recovery helper safety review is clean.
The staged diff remains empty.

The original 238-test integration race gate and other Go packages passed. The
corrected 320-file source freeze passed the complete 242-test integration race
gate and other Go packages. All 19 runner states are terminal with exit 0 and
no OOM; the enumerated coverage and source hashes match. Strict lint completed
with zero issues. The new Linux build completed
with exit 0 and no OOM, in a 512 MiB container. The full gate uses one bounded
1.5 GiB runner at a time and a disposable 256 MiB PostgreSQL instance.

## Retained native failure

The initial image was
`sha256:3e01c621cbb948e8527134191f7b360779bf53172e31b0581755fa73e29e1d76`.
Its explicit migration 31 rollout preserved the selected prior records. The
validated pre-migration dump SHA-256 is
`6dc25896e6be487894ea4ee0af7b56b7201d6ea18d37a584770cf4d70cea74c7`.

The first real Loom write failed before approval or dispatch: the description
repeated full documents and exceeded Loom's 4,000-byte preparation limit. The
initial preparation was 128,754 bytes. At that failure, native state confirmed
the exact operation was `failed`, its proposal expired with no result, and the
synthetic event had not been created. The original checkpoint and proposal are retained unchanged.
This is a Zinusya integration defect, not a reported upstream Loom bug.

The corrected description is bounded, while complete normalized arguments stay
separate. Recovery requires the known failed operation and absent event before
cancelling its saved paused fixture run and starting separately approved calls.
A timeout alone never authorizes a replacement operation. Each new run and
approval intent is persisted before submission; uncertain outcomes require
inspection of the saved identity.

## Native recovery acceptance and boundary

Native recovery passed using real local Zitadel and Loom with
a manually driven Codex provider, a synthetic future event, zero capacities and
100 syntactically valid unknown module identifiers. No registration, pass,
authority or money row changed in the selected preservation checks. The creation
approval survived a bot restart. Its first input returned HTTP
503 during Loom authorization; the operation remained prepared and the event
absent. A retry with the same run, approval and idempotency key succeeded. The
original run completed with one version-1 event receipt. The exact upstream
error code was not captured, so no more specific cause is claimed. The second
approved write replaced version 1 with version 2. Both runs
completed. Descriptions measured 1,144 and 1,149 bytes; complete normalized
100-module/100-tier arguments survived the broker. After another bot restart,
status, reconcile and replay returned each original receipt, including version
1 after the version-2 edit. Current configuration remained exactly version 2.
All three proposals remain: the failed original preparation without a result
and two successful operation receipts. The original checkpoint bytes and failed
proposal hash are unchanged.

The accepted image is
`sha256:fa34fa4e2d14a37b951ccd8b6a5b8fa265c57fe42dc0f02ac8b59406553ce321`.
The validated pre-description-fix backup SHA-256 is
`79ae7dabf1a0123edfd692b6b66a4f6122e008f035d9c46d082a0d7a2b0329f6`.
The additive schema remained 31 during this replacement. No native previous-binary
rollback or native COMMIT-reply fault was performed for this slice.
The temporary QA database, exact disposable volume and terminal test/build
containers were removed. The main owned stand remains available for later work.

The preservation check hashes 28 selected business/settings/audit tables plus
the three pre-existing event-configuration tables, excluding only the explicitly
saved synthetic event from the latter. This is a scoped preservation check,
not proof that every table in the database is unchanged. No Telegram messages
are sent by this native fixture. Browser CA trust, interactive zitadeltg,
production deployment, automatic allocation and financial lifecycle policies
remain separate gates.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
