# Mock Telegram text entity acceptance

Date: 2026-10-19. This fixes the fixture limitation exposed during
[specialist soon runtime QA](shuttle-massage-specialist-next-runtime-qa-2026-10-19.md).
That historical run and its failed observer/read-back evidence remain unchanged.

## Change

`sendMessage` now echoes explicit text entities in its response and persisted
message snapshot. `editMessageText` replaces the prior entities; plain edits with
omitted or empty entities clear them. Original request attempts and the pre-edit
journal retain their original values. Entity offsets and nested mention users
are forwarded unchanged. This covers explicit text entities; it does not add a
Markdown/HTML parser or new entity validation policy.

The [Bot API Message schema](https://core.telegram.org/bots/api#message) exposes
text entities; [sendMessage](https://core.telegram.org/bots/api#sendmessage) and
[editMessageText](https://core.telegram.org/bots/api#editmessagetext) accept them.
Only the local fixture and its behavioral tests changed. The Go candidate remains
schema52 / `mvp-59`; its image and production code were not rebuilt or installed.

## Verification

Two new localhost HTTP tests failed on the prior source with missing `entities`,
then passed after the five-line change. They check a link and nested user mention
with UTF-16 offsets after an emoji, response/snapshot/attempt agreement, edit
replacement, omitted/empty clearing and original pre-edit history.

The complete fixture/provider/QA suite ran on Windows: 24 tests, with only the
POSIX SIGTERM process test skipped. Linux then passed all24 tests without skips,
including that process lifecycle case. The first Linux run lacked the separate
readonly `counted_provider.py` mount and failed before starting that case; its
log/result are retained. Adding the required test dependency fixed the harness
without changing production source.

Both Linux test containers were networkless, readonly, bounded to256MiB/no extra
swap and2CPU, and removed after retaining logs and terminal state. Neither OOMed.
No primary or Loom developer service was restarted and no Telegram message was
sent. Independent scoped source review found no actionable findings.

Ignored evidence: `.cache/shuttle-work/mock-telegram-entities-v1-qa/`, including
pre-change source, Windows/Linux logs, the retained first Linux failure and the
successful `linux-tests-v2.json`. The full Go/River/domain suite was not rerun for
this fixture-only change; the earlier candidate acceptance remains separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
