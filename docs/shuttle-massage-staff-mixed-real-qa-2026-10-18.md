# Real Telegram specialist approval handoffs — 2026-10-18

Status: PASS for an agent-only access revocation, native-form-to-agent grant,
and agent-to-native-form revocation on the owned schema48 / `mvp-54` stand.
Real Telegram, IAM, Loom, broker tools, native form and PostgreSQL were used.
A manually driven provider supplied tool calls and final text. This proves the
integration and durable effects, not unattended model interpretation or production
readiness. No product source or installed image changed in this gate.

## Setup

All Telegram requests and confirmations used the existing account in
`@complynx_test_bot`. The existing `QA staff access - SYNTHETIC` event and its
Alex binding started active at version3, after the
[native staff/read/restart gate](shuttle-massage-staff-native-real-qa-2026-10-18.md).
The two event bookings remain synthetic fixtures, not real payments or clients.

The current account's local IAM grants were empty. A separate recorded cycle
created two temporary `zinusya.admin` project grants, then removed those exact
grants. A final IAM query confirmed the original empty grant list. Production
IAM, another Telegram account and Loom developer stands were not changed.

## Agent-only revocation

An ordinary Telegram request asked to revoke the current account's Alex access
without changing bookings or payments. Actual Loom broker reads returned current
binding, target and pending proposals before preparing the administrative call.
The visible approval displayed event, alias, account and scope in human terms,
with an ordinary Approve/Reject reply keyboard and no operation tags or raw JSON.
The binding still had version3 before consent. A free-text response,
“Да, отзови доступ alex.”, reached the same run and its exact approval resolver.
One canonical operation revoked the binding at version4. The delivered final
reply and reopened native form agreed with that result.

Run: `3692ca45-4d17-45ad-92f4-0a1f636e0c3d`.

## Form to agent

The real Mini App showed revoked version4 and prepared a grant for the same
account. Its preview displayed event-local expiry. The form did not apply it.
A Telegram request asked to continue that saved preparation through the dialogue.
The real administrative tool adopted its `existing_proposal_id` rather than
creating replacement consent. Free-text “Да, назначь доступ alex.” confirmed it.

The original canonical operation, proposal ID, arguments, request hash and expiry
were unchanged. A new interface operation was an alias of that same canonical
operation. Its single effect granted version5. Reopening the original form
recovered the completed original result and separately displayed current version5.

Run: `1d56ad3a-51a8-42c4-90d2-9cb05513b44b`.

## Agent to form

A Telegram request asked the agent to prepare a revocation for confirmation in
the form. Loom prepared it and waited for approval without changing version5.
The actual Mini App discovered “Review saved access change: alex · Revoke access”.
Selecting it adopted the agent's original proposal and displayed its exact terms
and original expiry. Native confirmation applied version6, revoked.

The next Telegram message stated that the form had confirmed the revocation and
asked to reconcile the original result without repeating a change. The same Loom
run completed against the original operation receipt. Its final response stated
that the form had already confirmed the revocation. There was still one canonical
operation and one effect for this action; neither reconciliation nor delivery
created another version or renewed consent.

Run: `2cbfc66a-7145-47de-add0-612b9a49014d`.

## Final checks and evidence

Each run has two completed bot tasks: request and steering input. Each has three
delivered outboxes: approval, input acknowledgement and final reply. All three
final replies were observed in Telegram. These nine deliveries are expected,
not duplicate final answers. Exactly three new canonical operations produced
versions4,5,6. Both mixed paths retain the original proposal parameters and expiry.
All booking rows equal the pre-test snapshot. Final Alex access is intentionally
revoked at version6; this account has no pass in this event and its global IAM grant list is empty.

The ten owned Compose containers stayed running with no OOM flag. No additional
containers were created for this gate; the reusable mock stands remain stopped.
There are no unfinished bot tasks. No commit, push or production change was made.

Ignored evidence: `.cache/shuttle-work/massage-staff-mixed-real-v1-qa/`:

- `agent_revoke.json`, `native_to_agent.json`, `agent_to_native.json`: actual run,
  proposal, canonical operation, alias, steering, result and delivery receipts.
- `iam-before.json`, `iam-changes.json`: empty baseline and exact temporary grants,
  with removal receipts and restored state.
- `final-state.json`, `checkpoint.json`: row and terminal-state acceptance.
- `native-to-agent-verification-failure.json`: preserved QA comparison failure.

## QA corrections and limits

Independent helper review found an uncertain mailbox publication replay window,
missing exact-consent matching and incomplete canonical/pre-consent assertions.
The driver now stops on an unresolved response intent, matches consent to its
current scenario, checks pre-consent state before resolution and verifies the
canonical effect and adoption aliases. Fresh final helper review found no
remaining actionable finding.

One completed native-to-agent effect first failed a QA equality assertion: the
public result omits SQL `created_at` and uses `Z`, while the row projection uses
`+00:00`. The driver was corrected to compare the complete result contract and
parse timestamps as instants. The same run resumed; the operation was not replayed.
A later attempted check of saved approval snapshots showed that polling had
replaced those snapshots after resolution. That check failed and is not claimed
as passed. Read-only verification instead compared the original immutable effect
timestamp with the actual steering-input receipt for both dialogue confirmations:
each single effect occurred after its received consent.

One Telegram locator timed out while scrolling to an older Mini App launcher.
The current accessibility tree identified the existing button, which opened the
form. No extra Telegram request, replacement task or new proposal was sent for
that browser observation failure.

This gate does not exercise a new restart, service outage, full live-controller
rollback, expired/stale proposals or unattended model decisions. Earlier reports
retain their own limits. Specialist notification preferences, self-reservation
and rescheduling remain separate work. The next integration gate is the remaining
order-evidence agent/mixed flow, preserving the earlier failed and expired order
consent rather than reviving it.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
