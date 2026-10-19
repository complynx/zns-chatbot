# Avatar proposal adoption — source and PostgreSQL QA, 2026-10-19

Schema58 / manifest mvp67 adds actor-owned operation aliases and the agent
prepare control `resume_proposal_id`. Native administrator repair routes and UI
are not part of this accepted stage. The installed primary remains schema49 /
mvp56; the retained built candidate image remains mvp65. Current source is not
installed in either or in production.

## Accepted behavior

- A new operation can adopt the same actor's pending proposal using exact original
  business arguments. Original proposal, description and five-minute expiry stay
  unchanged. Discovery returns the canonical proposal once; exact reads through
  an alias identify its canonical operation.
- Three concurrent confirmations through the original and two adopted operation
  IDs all return the same immutable result. The delivery changes version once.
  Status through each ID reconciles the canonical result, including its original
  attempt and receipt. Adoption itself changes no delivery and supplies no consent.
- Different arguments, proposal rebinding, another actor/organization, lost admin
  authority, local revocation, expired proposals and completed proposals are denied.
  Exact actor authorization remains required on historical replay.
- Agent preparation returns the original business arguments without the transport
  resume control. A retained control in confirmation must name the exact approved
  proposal. Existing prepare/confirm/status contracts continue to work.
- Migration58 preserves canonical proposal/result data. Empty rollback/reapply
  preserves an existing proposal. Nonempty aliases block rollback even after a
  completed resolution. The historical avatar rollback chain includes migration58.

## Verification

The final source passes22 root integration tests on real isolated PostgreSQL:
five adoption, four proposal recovery,11 prior resolution and two finalization
migration/rollback roots. A separate final run passes the two historical avatar
rollback roots. These are24 distinct PostgreSQL roots, with mock IAM/Telegram
peers. The final source also passes19 API roots on Linux Go1.27.2, scoped
golangci-lint with0 issues, and unchanged sqlc regeneration. Two fresh independent
textual-diff reviews found no actionable findings; the final review includes the
contract and safe rollout/rollback description.

All four PostgreSQL instances from this stage are archived and removed, including
failed/intermediate runs. Both temporary Linux SDK containers are removed. No
OOM occurred. Primary11-container epoch and domain preservation checks pass.
The index remains unchanged. No commit, push, production or primary installation.

## Retained failures and acceptance limits

The first PostgreSQL run passed21 roots and failed one HTTP fixture assertion:
it sent confirmation/idempotency fields to the read-only prepare envelope. The
server correctly rejected those extra fields with422. The corrected request
passed all22 roots on a fresh database. A subsequent lint run required reducing
a preparation function's cognitive complexity. The shared original-proposal
validation was extracted and the same22 PostgreSQL roots plus19 Linux API roots
passed again on that final source. Original failed evidence remains rejected.

No new actual Loom, real IAM, browser or Telegram acceptance is claimed. Earlier
real IAM/Loom repair acceptance covers the pre-adoption contract only. Native
administrator session fencing, native routes/UI and actual bidirectional Loom
handoffs are next. A fresh broker approval remains required; a discovered or
adopted proposal is never approval. Upgrade uses one application version at a
time; older binaries do not understand aliases. See the current
[resolution contract](shuttle-avatar-delivery-resolution.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
