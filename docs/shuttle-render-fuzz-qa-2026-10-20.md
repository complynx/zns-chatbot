# Markdown/entities bounded fuzz QA, 2026-10-20

The unchanged `internal/render` implementation passes one bounded `FuzzRender`
run on Windows/amd64 Go1.27.2: six baseline seeds, two workers,144872 executions,
120 new interesting corpus inputs and31.593 seconds elapsed. These counts are
fuzz executions and interesting inputs, not distinct-input or coverage percentages.
A fresh independent adversarial textual review finds no actionable issue.

The exact target is:

```sh
go test ./internal/render -run '^$' -fuzz '^FuzzRender$' -fuzztime=30s -parallel=2
```

The properties validate each emitted chunk's nonblank UTF-8 text,4096 UTF-16-unit
limit, entity ranges/boundaries and allowed nesting. Mention entities may reference
only the caller's supplied synthetic user7. Plain rendering emits no entities,
preserves eligible short UTF-8-normalized input verbatim and preserves its
nonwhitespace content across longer splits. Invalid UTF-8 is replaced with U+FFFD. Imperfect Markdown, emoji, code and mention links are seeds.

The check uses a separate worktree; formatter source hashes stay unchanged and no
failing corpus file or product fix is produced. Normal unit tests are excluded by
`-run '^$'`; this is not a replacement for the full Go/race CI suite. It does not
prove semantic preservation of every Markdown document, arbitrary worst-case
performance, Telegram server acceptance, delivery fallback or production readiness.
No container or external service is created. Local proof and log remain under
`.cache/shuttle-work/render-fuzz-2026-10-20-v1/` in the primary checkout.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
