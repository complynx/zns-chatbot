# Native pair assignment: canonical continuation foundation

Status: DB/API component checks pass for source schema 62 / `zinusya/mvp-70`.
This source is uninstalled. The owned primary remains schema 61 / `mvp-69` as
accepted in the [solo assignment installation report](shuttle-native-pass-assignment-primary-qa-2026-10-19.md).
The native pair workspace, session/IAM fences, native action history and complete
four-path runtime acceptance remain next in this same pair workflow.

## Shared operation contract

The existing six-field `PairAssignment` and atomic two-pass transaction remain
the business command. Agent preparation accepts optional `resume_proposal_id`
as transport metadata. It binds the caller's new opaque operation key to that
caller's original pending proposal. The original proposal ID, arguments, quotes,
description and expiry stay unchanged. New preparations also freeze the event
timezone; older documents can omit it.

Preparation reserves no capacity. Adoption grants no consent and rejects changed
arguments, foreign owners, expired/completed proposals or occupied keys. The
preparation response contains only the original six business arguments. A fresh
exact confirmation must name the same proposal. Supplying a resume field only
to the write endpoint does not create an alias or authorize execution.

Confirmation and status resolve aliases to the canonical operation. Exactly one
canonical receipt commits with both passes. After uncertainty, reconcile that
operation; a missing result leaves the outcome unknown. Completed receipts stay
readable after expiry or sales closure under current authority. They describe the
original outcome; read current pair/pass state separately.

Pair assignment retains its existing permission: current global administrator
or administrator of the specific event. Solo assignment remains global-admin
only. Account revocation and event authority are checked before replay or data
exposure. This foundation does not yet add a native privileged session boundary.

## Proposal discovery and storage

The agent API adds `/internal/agent/pairs/assignment_proposal` and
`/internal/agent/pairs/assignment_proposals`. Exact reads are actor-owned and
check current event authority. Pending lists filter accessible events before
the maximum 20-row limit and paginate by proposal UUID, without implying
allocation priority. An active actor with no remaining event/global authority
receives an empty list; revoked or mismatched actors are denied.

Migration 62 adds direct transport aliases to canonical pair preparations.
Aliases cannot chain or shadow canonical keys. The namespace includes all
pair receipt kinds, including invitation, decision and dissolution receipts.
Fresh participant actions reject reserved assignment keys before effects;
existing participant receipts still replay by their exact original keys.
Database triggers also protect older insert shapes. Populated aliases block a
schema downgrade until deliberate export and reconciliation. No schema-62
binary installation/rollback acceptance is claimed here.

Canonical confirmation acquires transport and canonical advisory locks before
the event, then actor authority and the canonical proposal row. This matches the
receipt trigger's namespace lock and avoids inversion with an original-key
confirmation. Existing capacity, membership, version, legal-details and quote
checks still run before the atomic two-pass write.

## Verified scope

The same 45 PostgreSQL root scenarios pass before and after the participant
receipt extraction, with no failures or skips. They include the new DB/API
continuation and migration-retention cases plus existing pair/native membership,
privacy and atomicity paths. Eleven focused post-lint scenarios also pass.
DB/API unit checks, lint and isolated sqlc compile/generate pass; all three
generated Go files reproduce byte for byte. These local Go checks use Go 1.27.2
on Windows with `CGO_ENABLED=0`; they do not establish Linux/race acceptance.

Two review rounds each ended with clean independent correctness and code-quality
reviews. The first lint run's seven findings remain recorded; embedded-field
spacing, shadowing, a shared schema-field constant and exact participant receipt
extraction resolved them. This is source/component evidence, not actual native
form, real IAM/Loom or unattended-agent acceptance for pair assignment.

The bounded component PostgreSQL and its volume were archived and removed;
final stop was exit 0 without OOM. Read-only custody confirms the primary's
11 services and 134 Loom runs unchanged. Local ignored evidence is under
`.cache/shuttle-work/native-pair-assignment-v1/`: `scope-v2.json`,
`pg/pg-behavior-v2.json`, lint/unit logs, `sqlc-verify/result.json`,
`primary-preserved.json` and `pg/postgres.json`. No staging, commit or push was
performed for this component stage.

Next: add the native pair administrator session, owner-scoped form reads and
immutable native history, then the two-recipient form and the complete native,
agent and both handoff runtime paths. The broader
[registration/pass acceptance matrix](shuttle-registration-pass-parity-plan.md)
remains open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
