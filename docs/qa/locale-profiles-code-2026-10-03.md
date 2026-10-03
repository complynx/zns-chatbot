# Independent Code QA 396

Verdict: PASS for the three-file source scope. No actionable findings.

Candidate: `e5b0af6fd07cdb8c83774fc59c83984e3a4d4869`.
Base: `22ee814eb9d70e900a1e9dc909ac70e898eedeaf`.
Worktree: `.worktrees/c-locale-profiles-20261003`.
Reviewer: fresh Codex agent, GPT-6 family as identified by the supplied harness instructions; exact runtime model slug is unavailable. No Claude was launched or probed.

## Requirements and scope

Original Functional F04 requires external Telegram `language_code` inputs be/by/uk/ua/pl, controlled primary/fallback catalog absence, actual UI/error/persisted-locale observations, and preserved EN/RU manual and agent behavior. The existing preference menu remains EN/RU. This review covers catalog-data stand preparation, not additional translations or menu choices.

Reviewed exact candidate/base diff and approved base contracts in AGENTS.md, docs/go-migration.md, docs/architecture-refactor-plan.md, docs/architecture-ownership.md and docs/code-quality.md. Relevant immutable source inspected: i18n registry, fallback/alias handling, catalog definitions and existing tests; account preference normalization/transaction source; bot language renderer and initialization consumer; sandbox lab input transport. No author reports, prior QA findings, management material or audit/history searches were consumed.

Changed paths are exactly three additions:

- `docs/locale-profile-stand.md`
- `platform/internal/i18n/locale_profile_internal_test.go`
- `platform/testdata/locale-profiles/prepare.sh`

## Source evidence

- `prepare.sh:5-9` restricts profiles to the three declared values. Output and overlay source paths are literal owned Linux paths. `mkdir` refuses reuse of an existing profile directory.
- `prepare.sh:13-16` checks complete EN/RU source hashes. Read-only hashing of the clean candidate worktree confirmed EN `7db018e92d3ec8db092e8fcef2fc3b9506fea0a40bfeddbe1d237b4a898954d7` and RU `2976ae412c63c52e5882dc67ec0beb2d4d09e6d4096a037fbe3d784d0792870d`.
- `prepare.sh:21-27` emits only selected catalog replacements and removes only the single guarded `LanguageChoose:` row. Primary-gap emits an empty replacement map. Fallback-gap changes RU only; missing-all changes EN and RU. Other catalog entries, locale registration, production resolver, aliases, account source and persistence remain unchanged.
- `locale_profile_internal_test.go:1-46` is opt-in source under `synthetic_locale_profile`. It validates the supported locale set, all five raw input fallbacks, exact fallback text or `ErrUnknownMessage` with empty text, and retained EN/RU current-language entries using the actual production `Translate` function. No resolver substitute, runtime fixture switch, dependency change or new lint exclusion is introduced.
- Existing `locale.go:63-104` supplies by/be and ua/uk normalization and RU compatibility; `language_transaction.go:54-94` preserves canonical initial tags and prevents initialization from overwriting a nonempty preference. `bot.go:178-179` forwards incoming language through that initialization path.
- Existing `bot/language.go:24-77` uses the stored preference, returns translation errors before constructing/sending a partial prompt, and derives menu buttons from the EN/RU supported locale registry.
- Documentation separates incoming tags from explicit choices, requires empty initial actor preference and isolated scenario state, describes data gaps accurately, and requires executable/source/profile/overlay bindings before startup. It retains sole stand ownership, frozen-stand protection and separate lifecycle permissions.
- Documentation explicitly preserves ordinary catalog completeness and pinned lint gates, adds tagged lint only for the complete primary-gap catalog, and does not promote the deliberately incomplete tagged test into an ordinary production gate replacement.

## Verification and limitations

Before and after review, `git status --porcelain=v1` for the candidate worktree was empty, `git rev-parse HEAD` equaled the candidate, and `git merge-base --is-ancestor BASE CANDIDATE` exited 0. Exact diff contains the three additions above. `git diff --check BASE CANDIDATE` exited 0.

Only Git/source inspection and read-only file hashing ran. No tests, lint, build, Bash preparation, Docker, SQL, stand operations or native Windows quality gates ran. Tests were reviewed as source only. Prepared overlay/binary immutability and startup composition must be observed by the owner at runtime; source instructions alone do not prove these bindings. External UI/error behavior, actor isolation, actual preference persistence and EN/RU agent/manual behavior still require Linux gates and independent Functional QA. This PASS is neither full stage acceptance nor Functional acceptance.

Written by Codex (GPT-6 family/Codex agent harness)
on behalf of Daniel Drizhuk
