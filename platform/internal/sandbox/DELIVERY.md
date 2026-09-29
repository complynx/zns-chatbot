# Synthetic delivery stand

The local Bot API accepts numeric JSON or numeric-string `chat_id` for private fixtures 101, 202, 303. It also accepts fixed synthetic destinations:

| ID       | Alias            | Type       | Topics   |
| -------- | ---------------- | ---------- | -------- |
| -1009001 | @sandbox_channel | channel    | none     |
| -1009002 | @sandbox_forum   | supergroup | 101, 102 |

Select these destinations in the existing UI to inspect delivered messages. Forum messages show their topic number. Sending manual user inputs to these destination views is unsupported; use an administrator private conversation to invoke product delivery. No external Telegram messages are sent. Unknown destinations and topics fail with Telegram-shaped 400 errors. Private fixtures remain unchanged.

Text delivery accepts plain text, the existing MarkdownV2 subset, and these bounded additional modes:

- HTML: b/strong, i/em, u/ins, s/strike/del, code, pre, blockquote, tg-spoiler, span class="tg-spoiler", and a href. Formatting can nest except duplicate types and code/pre nesting. Links allow HTTP(S) and tg://user?id=positive-integer. Text supports the four Telegram named character references and numeric references. Other attributes/elements, mismatched/unclosed tags, and unsafe URLs fail.
- Legacy Markdown: non-nested bold, italic, inline code, fenced pre, links; documented escapes outside entities. This delegates entity decoding to the existing V2 parser. Nested entities, escapes inside entities, malformed delimiters and unsupported link targets fail. Literal punctuation remains literal.

This is deliberately not full Telegram grammar. HTML pre/code language nesting, expandable quotes, custom emoji/date tags, other URL schemes, empty formatting tags and some otherwise valid legacy backslash/code cases are rejected. Spoilers have a visible background label; the stand does not simulate Telegram's spoiler reveal animation. Source is bounded to 32 KiB; visible text uses the existing 4096 UTF-16-unit validation. Explicit entities and unsupported request options remain rejected rather than silently ignored.

Message chat metadata, topic IDs, rendered text and entities use the existing durable fake_state snapshot, restored by New. Without PostgreSQL (unit mode) state is intentionally in-memory. Channel/forum views have no private interaction history. This slice does not emulate channel membership, forum management, Telegram network permissions or update generation from channel readers.

Contract reference: https://core.telegram.org/bots/api#formatting-options (checked 2026-09-26).
