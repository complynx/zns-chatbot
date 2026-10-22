# Profile name entry

## Current source contract

`/start` enters the agent dialogue if this bot's current active linked user lacks
separate first/last profile fields. The dispatch can adopt filled names from a
verified existing IAM account before preparing its prompt. A filled local profile
keeps `/start` as native navigation, including during Loom downtime. This intake
choice is a routing hint; dispatch still checks ownership and authorization.
Duplicate Telegram updates return the original accepted task even if the profile
was completed between deliveries. Previously admitted tasks keep their route.

New `/name` and `/name ...` messages are agent input. The agent uses the shared,
bounded name expectation and asks a conversational question. A normal reply,
including an explicit reply to that message, is agent input. The agent sees the expectation in profile context and decides
whether the message supplies a name, asks a question, cancels something or is
unrelated. The transport never writes a name merely because `reply_to` points at
a name prompt. Attachment captions remain attachment input to the agent.

The conversational prompt, owner expectation and profile version remain durable. The
agent can use profile tools to complete the exact current expectation/version.
A stale expectation or receipt is not current authorization. Canceling in prose
is an agent decision, not a client keyword or hidden marker.

The agent receives Telegram first name, last name and username as untrusted hints.
It may offer a plausible real name through an ordinary reply-keyboard choice,
or ask without suggested choices. Presenting a suggestion does not save it.
The user may correct it, give another name or cancel in ordinary text. Compound
name components remain intact; ambiguous divisions require clarification.

For agent-free changes, the Mini App profile form uses separate first/last fields.
It shares the versioned profile and closes the current name expectation when
successful. It works while Loom is unavailable and enters bounded history for
later reactive agent context. Merely creating an expectation does not launch a
proactive run. Older already-admitted native name tasks retain their original
handler for recovery; new messages cannot enter that handler.

The implicit reply-routing helper, receipt-to-name binding and corresponding
checkpoint structure were removed. The expected field identity now comes from
an explicit native command/form or the agent's owner-bound API call, not from
guessing the meaning of a text/media reply.

## Acceptance boundary

This source change is part of the [conversational adapter](shuttle-agent-reply-keyboards.md).
Real-Loom/model QA remains required before claiming natural-language completion.
The HTTP-peer tests exercise agent-routed commands, explicit owner tool calls,
plain keyboard suggestions, unrelated text/media, cancellation text and restart
recovery. The provider is scripted: these tests do not prove model judgment of
real names. First-contact routing, existing IAM-name reuse, restart continuation
and completed-profile navigation have scripted-provider tests. Real-model
first-contact onboarding remains an acceptance gate.
The main owned stand still has the older image; no deployment or production
acceptance is implied here. The [historical native reply evidence](history/shuttle-native-name-replies-2026-10-16.md)
is retained separately and does not prove the new agent-driven behavior.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
