# Profile name entry

## Current source contract

`/name` opens the shared, bounded name expectation and presents a Telegram
ForceReply prompt. A normal reply, including an explicit reply to that message,
is agent input. The agent sees the expectation in profile context and decides
whether the message supplies a name, asks a question, cancels something or is
unrelated. The transport never writes a name merely because `reply_to` points at
a name prompt. Attachment captions remain attachment input to the agent.

The native prompt, owner expectation and profile version remain durable. The
agent can use profile tools to complete the exact current expectation/version.
A stale expectation or receipt is not current authorization. Canceling in prose
is an agent decision, not a client keyword or hidden marker.

For agent-free changes, `/name Full name` writes the explicitly supplied name.
The Mini App profile form remains an explicit field mutation. These routes use
the shared versioned profile and close its name expectation when successful.
They work while Loom is unavailable and enter bounded history for later reactive
agent context. Merely creating an expectation does not launch a proactive run.

The implicit reply-routing helper, receipt-to-name binding and corresponding
checkpoint structure were removed. The expected field identity now comes from
an explicit native command/form or the agent's owner-bound API call, not from
guessing the meaning of a text/media reply.

## Acceptance boundary

This source change is part of the [conversational adapter](shuttle-agent-reply-keyboards.md).
Real-Loom/model QA remains required before claiming natural-language completion.
The main owned stand still has the older image; no deployment or production
acceptance is implied here. The [historical native reply evidence](history/shuttle-native-name-replies-2026-10-16.md)
is retained separately and does not prove the new agent-driven behavior.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
