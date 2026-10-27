# Agent dialogue and operation notifications

## Upstream contract

The client follows Loom's [conversational-input contract](https://github.com/complynx/loom/blob/fc6c5747a4359ff94a58071054e124ba1449cc89/docs/conversational-input.md).

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
Plain name replies now use the same agent path; explicit native writes remain.
The failed name-routing run, caused solely by comparing fresh
ObservedAt timestamps, remains in conversation-adapter-name-routing-qa/.
