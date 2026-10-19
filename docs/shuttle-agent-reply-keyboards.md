# Agent dialogue and operation notifications

## Upstream contract

The client follows Loom's [conversational-input contract](https://github.com/complynx/loom/blob/fc6c5747a4359ff94a58071054e124ba1449cc89/docs/conversational-input.md),
merged in main at fc6c5747a4359ff94a58071054e124ba1449cc89 (PR15). The feature
commit is72bb68c. The remote main revision and ancestry were verified using an
isolated bare repository; the Loom developer's checkouts and stands were unchanged.
The earlier [proposal](loom-conversational-input-proposal.md) is historical.

Run creation requests `interaction.reply_mode=agent`, questions
`choices_and_text`, and `approvals=true`. The client requires the accepted run to
echo agent mode. It fails clearly if that mode is absent; no protocol fallback is
used. This is a breaking transition: old tagged runs are not a compatibility target.
Durable business records, operation receipts and normal task recovery remain.

## Text and native actions

Ordinary text, attachments and plain suggested replies enter the same run as
`kind=steering`. No text suffix, hidden marker, caption lookup or keyword grants
approval. `Yes`, `No`, `Cancel`, `/cancel`, conditions and questions reach the agent.
Choice-only questions do not prevent free text. The agent may clarify while the
same operation remains blocked. Completing a data expectation is not consent.

Reserved native commands such as `/menu`, `/register`, `/profile` and `/name`
retain their normal native paths. This applies equally to typed text and an
identical reply-keyboard label. A media caption remains attachment input, including text resembling a command.
Native actions enter bounded history for the next reactive
agent turn; they do not consume the current dialogue question. This preserves
agent-free use and switching between native and agent paths. Ordinary captions
are not resolved by matching them against a pending prompt before command routing.

A question owns the conversational reply keyboard ahead of a coexisting approval.
Ordinary agent approvals can suggest plain Approve/Reject captions. A method's
protocol approval uses operation-bound inline callbacks instead; these can work
while a subsequent question is displayed. External business notifications and
native forms retain their own structured callbacks. They do not answer dialogue
questions or turn chat text into binary decisions.

The adapter advertises native callbacks, so critical methods use protocol buttons.
It does not implement the exact-text fallback, in which any other message rejects
an action. Unknown approval modes do not produce confirmation controls.

For changed parameters, the merged Loom contract rejects the old proposal, creates
a new preparation and requires a fresh user message. Consent is not transferred
between preparations. Runtime authorization, expiry, binding and operation recovery
remain authoritative after the agent interprets text.

A reply reference to a native name prompt never authorizes a profile mutation.
Plain text, cancellation and media go to the agent; explicit `/name Full name`
and the Mini App keep agent-free name changes. See [name entry](shuttle-telegram-name-entry.md).

## Presentation and recovery

Outgoing prompt rows retain private transport metadata with question/approval IDs.
The typed Telegram sender strips it from the wire. When Telegram provides an
explicit reply reference, the adapter looks up only actually delivered rows in
that owner's conversation and forwards historical IDs with the opaque reply
reference. Unknown references remain opaque. Ordinary text does not invent a
reply reference. A reference is context, never permission or proof of consent.

A persisted keyboard revision gives every ownership transition and retirement a
durable delivery identity. Returning to the same blocked action after clarification
must restore its keyboard; retiring it again must remove that restored keyboard.
Stable operation identity is separate from presentation identity. Existing send
gates, wire receipts and ordered outbox delivery still suppress outdated markup.

The exact input body, presentation and idempotency key are frozen before sending.
Accepted input ID/context are saved in the task receipt when supplied by Loom.
A lost receipt is reconciled with the same body/key, including after restart or
parent completion. An HTTP receipt means accepted input, not approved action or
successful business effect. Questions and approvals can coexist in a run snapshot.

The old implicit native name-reply router and its checkpoint were also removed.
Review found it could turn a cancellation reply or media caption into a name write.
Plain name replies now use the same agent path; explicit native writes remain.
Fresh complete source review found no actionable findings. A separate review of
the corrected stored-state assertions also passed; observation timestamps are not
stored user data. The failed name-routing run, caused solely by comparing fresh
ObservedAt timestamps, remains in conversation-adapter-name-routing-qa/.

## Acceptance boundary

The source adapter passed25 root scenarios /44 pass events under Linux/race,
with real PostgreSQL and mock IAM/Loom/Telegram. No failures or skips occurred;
all805 frozen inputs and ten main-service fingerprints were unchanged. Evidence
is in ignored .cache/shuttle-work/conversation-adapter-complete-qa/.
The exact labelled containers and their volume were removed, terminal0/noOOM.
Database cleanup was immediate SIGQUIT, not graceful-restart acceptance.

Independent source review found two keyboard lifecycle defects: restoring an
unchanged approval after clarification, and retiring that restored keyboard again.
Both now use durable presentation revisions. A real-PG failing restoration
regression is retained in conversation-transition-repro-qa/. The earlier
conversation-transition-before-fix-qa/ failure was a test helper observing stored
markup before actual wire delivery; it is not classified as that product defect.
Earlier passing20/21/22-root snapshots are retained separately and do not replace
the final25-root evidence. No failed receipt was rewritten as a pass.

The native command boundary is covered by a reserved-command suggestion, native
profile read without a new Loom input, then steering into the same run with that
manual action in context. This is transport acceptance, not language interpretation. The later
[merged runtime gate](shuttle-conversational-runtime-qa-2026-10-16.md) installed the
port on the owned stand at bot schema36/Loom schema4, after the full Linux/race
suite. Actual Telegram plain-caption and menu-to-free-text continuation passed
with a manual provider. The direct probe required same-session auth recovery and
its report later needed evidence reverification; both limitations are documented.
RU/EN real-model interpretation and full mixed business/forms paths remain open.

The [archived tagged-keyboard record](history/shuttle-tagged-keyboards-2026-10-16.md)
preserves previous stand state, failed receipts and recovery evidence. Its parser
and compatibility expectations were removed from current code/tests deliberately.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
