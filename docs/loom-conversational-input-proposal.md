# Loom: conversational replies while an action is blocked

Status: historical proposal, superseded by the merged Loom contract on 2026-10-16.
See the [current adapter guide](shuttle-agent-reply-keyboards.md) for the final wire
format and implementation/acceptance boundary. Loom main fc6c574 contains the
conversational feature. Unlike the proposal's optional consent transfer, the
merged contract requires a fresh message after amended parameters. The observed
gap below describes the earlier source snapshot; it is not a current blocker.
The original requested behavior and examples are retained below as design history.

## Goal

Zinusya sends user text to the agent. The agent interprets permission, refusal,
conditions, clarification and cancellation from the conversation and the latest
pending question. A reply-keyboard caption is only suggested text: clicking it and
typing the same words have the same semantics. No visible or hidden operation tags
are embedded in that text. No label/keyword parser in Zinusya decides approval or
cancellation. Native forms and external operation notifications retain separate,
structured callbacks tied to their own operations.

Examples:

- `Yes, do it` may permit the operation currently discussed.
- `Yes, but only for one person` must not approve the original two-person action.
- `How much will it cost?` is a clarification, with no approval effect.
- `No` refuses the proposed action.
- `Cancel`, `Stop` and equivalent prose are interpreted by the agent. If the target
  is ambiguous, it asks which action the user means.

## Observed integration gap

Source inspection of the sibling Loom checkout, not a runtime reproduction:

- `internal/store/execution.go`, `addInput`: steering is saved, but only `answer`
  and `approval` inputs queue/wake a waiting run.
- `applyInput`: an approval accepts only `approve` or `reject` for the pending
  approval ID. It does not interpret user prose.
- `applyAnswer`: free text depends on the pending question's `AllowText` flag.
- `internal/runs/engine.go`, `handlePending`/`dispatchPending`: pending calls are
  processed before normal continuation; existing steering can supersede calls.
  Reusing that mechanism must not accidentally execute or discard a blocked effect.

Changing only the Telegram keyboard or submitting steering without waking the
agent does not implement the desired dialogue. This is a requested capability;
the current behavior is not classified as a confirmed Loom bug.

## Required behavior

1. Add an opt-in conversational interaction mode. Preserve current protocol mode
   for existing clients. The client must be able to discover/negotiate support;
   unsupported mode must fail clearly instead of silently falling back.
2. In conversational mode, accept ordinary user text while a run waits for either
   a question or an approval. A choice-only question cannot prevent a clarification
   or cancellation request from reaching the agent.
3. Acceptance durably records the exact text, its authenticated source, input ID,
   ordering and the pending interaction context observed at acceptance. Queue the
   continuation atomically with acceptance. A lost HTTP receipt must not lose or
   duplicate that input. Preserve available presentation/reply context separately
   from text: the last question actually delivered to the user, message reply
   reference, and delivery/acceptance ordering. A server-current revision alone
   does not prove which displayed question a delayed reply refers to. If context
   leaves the intended action ambiguous, the agent must ask before resolving it.
4. Wake the same agent run to interpret the input. Supply the actual pending
   question/action, complete material parameters, expiry and the relevant dialogue
   history. Old approvals and tool output are context, not new user consent.
5. Keep the pending business effect blocked during interpretation. Do not replay
   the pending write before processing the user's response. Receiving a message,
   waking the agent or producing an assistant answer does not grant approval.
6. The agent may ask a follow-up, refuse, approve the unchanged action, prepare an
   amended action, or request cancellation with an explicit scope. A follow-up
   keeps the action blocked. Amending material parameters invalidates the old
   consent and requires a new bound preparation/consent. The current message can
   itself supply explicit consent for the amended action if it fully specifies
   what is permitted; otherwise ask a follow-up. Do not require another question
   merely because the permission was expressed in prose.
7. A runtime-controlled resolution mechanism applies the agent's interpreted
   decision to the exact pending interaction. Its name/shape is negotiable. It
   must refer to a real accepted user input and the expected interaction revision;
   it cannot approve an arbitrary operation or consume old consent for a new one.
   No resolution may approve from tool output, model initiative or quoted history.
8. Resolution rechecks current owner, permission, expiry, operation identity,
   parameter binding and preconditions. Permission revocation wins over an earlier
   interpretation. Existing operation idempotency and unknown-outcome recovery
   remain authoritative. Agent interpretation is not an authorization bypass.
9. Process accepted inputs in a stable order. Do not dispatch an approved effect
   ahead of an already accepted relevant correction/cancellation. After the effect
   crosses its dispatch boundary, report the actual outcome; cancellation cannot
   promise rollback. A message accepted later belongs to the actual state then.
10. Persist interpretation/resolution and their input links. On restart, resume
    without a second business effect or applying the same decision to a different
    pending question. Unknown dispatch outcomes require reconciliation before a
    replacement operation can execute.
11. Expose the current dialogue wait and its suggestions so Zinusya can replace or
    remove the reply keyboard. A clarification may replace the displayed question
    while retaining the blocked operation. A detached notification must not become
    the active conversational question.
12. Keep explicit protocol controls for native operation callbacks and operational
    emergency stops. Those controls do not require model interpretation and remain
    distinct from conversational words such as `Cancel`.

## Possible API shape, not a fixed contract

For example, run creation could negotiate `interaction.reply_mode = "agent"`.
The existing run input endpoint could accept:

```json
{"kind":"message","text":"Yes, but only for one person"}
```

Use the existing request idempotency mechanism. The server records the pending
interaction context separately; Zinusya does not insert IDs into user text. An
input receipt should identify the durable input and its ordering. Neither the
message nor its receipt means `approved`.

An internal agent tool could resolve a pending interaction with a structured
reference to the accepted input, expected interaction revision, exact operation
binding and interpreted decision. These fields are runtime metadata, never text
steganography. The developer may instead extend existing steering/answer/control
mechanisms if they meet all behavior above. Do not require a second classifier
service or a new long-lived run for each reply.

The final documentation must define: capability negotiation; input and receipt
schemas; waiting-to-interpretation-to-resolution state transitions; agent tools;
revision/expiry checks; queued-input ordering; idempotency/conflict behavior;
restart and unknown-outcome handling; old-client compatibility. Zinusya's adapter
and tests are updated to that agreed contract, not to this illustrative schema.

## Acceptance

First, deterministic integration tests prove protocol and business-effect safety:

- A waiting question/approval receives text and the same run processes it.
- A caption and identically typed text produce equivalent agent input, without
  hidden tags or client-side decision mapping.
- Clarification causes zero write dispatches and can lead to a follow-up question.
- Qualified consent changes the proposal, with zero old-parameter dispatches.
- Agent-resolved consent permits exactly the bound unchanged operation; refusal
  does not dispatch it. Missing input, wrong owner, stale revision, expired approval
  or changed parameters cannot resolve successfully.
- Conversational cancellation stops the identified pending work; ambiguous scope
  asks a question. Already dispatched/uncertain work is reconciled and reported.
- A correction accepted before dispatch prevents use of superseded consent.
- After question/action replacement, delayed captions and typed replies, including
  delayed Telegram delivery, retain available presentation context. A bare `Yes`
  cannot silently authorize a replacement action that the user has not seen;
  ambiguity leads to clarification, with zero business dispatches.
- Lost input/resolution receipts and restarts preserve one logical resolution and
  at most one confirmed business effect. Same idempotency key with a different
  body conflicts. Concurrent continuations cannot bypass the dispatch gate.
- An unrelated external operation callback does not answer the dialogue question.
- Existing protocol-mode clients retain their current behavior.

Then real-model QA proves interpretation in context, in Russian and English:
permission in natural language; negation; a condition attached to permission;
clarification; refusal; cancellation by caption and prose; ambiguous cancellation;
a reply after a replacement question; and continuing through native/agent paths.
A scripted provider proves transport only, not language interpretation. Record
model/version and outcomes; do not describe probabilistic interpretation as a
formal safety guarantee. Authorization and effect gates remain deterministic.

## Responsibility split

Loom owns the conversation, interpretation, blocked-call continuation and durable
resolution contract. Zinusya owns Telegram presentation, durable inbound delivery,
business APIs and their authorization/preconditions, and records native actions
as agent context. The Zinusya adapter must wait for negotiated support before
claiming this conversational mode is available.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
