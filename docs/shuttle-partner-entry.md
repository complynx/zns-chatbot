# Partner contact codes and Telegram entry

`zinusya_pairs.partner_code` returns the authenticated caller's stable local
account UUID as `partner_code`. It accepts no arguments, including no target ID.
It also returns `share_url` when Telegram `getMe` confirms the configured bot ID,
bot flag and a safe username. The metadata request has a two-second timeout; a
failed or unverified lookup leaves a usable plain code. Authorization is checked
again after that network request. There is no new database table or migration.

The link is `https://t.me/<bot>?start=partner_<32 lowercase UUID hex characters>`.
A changed Telegram username may invalidate an old URL; the account code remains
stable. Codes are public addressing information. They are not credentials,
proof that someone shared the link, an invitation, or consent.

Only an exact private `/start partner_<code>` text message is recognized. Attachment
captions, malformed/noncanonical codes, extra tokens and group commands do not
produce partner context. The authenticated Telegram sender remains the actor.
The normalized durable task stores the optional reference, and the checkpointed
Loom input contains an explicitly untrusted target hint, including whether it
refers to the caller. New runs and active-run follow-up inputs use the same rule.
Previously accepted tasks without that optional field retain their behavior.

Opening a link creates no registration, invitation, allocation hold, event pin,
name change or payment. The agent can explain a self-reference or ask which event
and action the opener intends. After explicit intent and the allocation-hold
explanation, the opener can invite the code owner through the existing directed
invitation operation. The target then separately accepts. All existing eligibility,
version, permission, operation-receipt and recipient-consent checks still apply.

There is no profile directory, username/TG-ID search, rotating bearer token or
automatic pairing. Unknown account references are treated as untrusted input;
intake does not look up or disclose their profiles.

Implementation and acceptance were planned in
[the bounded implementation plan](shuttle-partner-entry-plan.md). Source tests,
native service checks and actual Telegram client evidence are separate gates;
see [QA record](shuttle-partner-entry-qa-2026-10-15.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
