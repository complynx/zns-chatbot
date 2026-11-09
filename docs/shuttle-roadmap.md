# Shuttle roadmap

Shuttle is the standalone Go implementation of zns-chatbot / ЗиНуСя. The [specification](shuttle-spec.md) defines the target behavior; the [architecture](shuttle-architecture.md) defines ownership. The application uses the consolidated [version-76 baseline and ordered additive migrations](shuttle-schema.md).

## Operating phase

ZiNuSya is deployed in production. Treat further work as changes to a live system
with retained users, settings, tasks and delivery receipts. A source change or green
CI does not prove that its image or schema is installed. Exact live revisions,
installation settings and rollout receipts belong in local operator documentation.

Every release must satisfy the [migration and compatibility gate](shuttle-qa.md#live-upgrades-and-restarts).
Ordinary startup must apply pending application and River migrations before runtime
work, then check the runtime schema and access. This requirement needs acceptance
on the candidate build; do not infer it from a previous release's startup checks.

## Current scope

The source includes identity and profile entry, durable task routing and recovery, registration and pair/pass workflows, payment evidence and human review, service orders, meals, massage, avatars, knowledge and historical account import. These features use shared native and agent business operations. Bounded acceptance exists for these paths; it does not establish every role, contention window or external-service failure combination.

[Sticker and custom emoji descriptions](shuttle-symbols.md) add active reaction input, a bounded popularity catalog and a separate visual-description worker with bounded parallelism. Optional bot reactions use exact message receipts and independent delivery recovery. New conversational runs require the Loom output-policy capability for same-run custom emoji rejection and correction. The descriptor requires Loom's native-file execution profile and a dedicated account. Full animation-description acceptance is incomplete. Controlled consumer tests do not replace actual-model acceptance.

Controlled orchestration and actual-model, browser, Telegram and provider observations retain separate scopes. A successful local path does not itself authorize release or establish production readiness. Detailed run evidence and installation configuration are maintained outside tracked product documentation.

[Agent skills](shuttle-agent-skills.md) defer domain procedures until needed while
keeping identity, language, memory, consent and recovery in the core prompt.
The integration is opt-in and requires Loom's skills protocol; legacy discovery
and prompts remain available for compatibility.

## Remaining work

1. Complete the human real-API walkthrough, including real Telegram audio and the relevant native/agent handoffs. Reuse established evidence where the behavior and source remain applicable; do not rerun the entire matrix merely because a dependency changed.
2. Resolve failures or unmet requirements found by that walkthrough at the narrowest responsible layer, with focused behavioral proof and independent review.
3. Gate each production update on exact source/CI, startup migration and populated-data compatibility, current authority, preservation and recovery evidence. Check the installed revision after rollout. Publication and production changes require their applicable authorization; a merge can trigger an automatic rollout.

## Working rules

Do not rewrite deployed migrations, reinitialize live settings or replay imports to make a new build start. Add a reviewed forward migration and verify preserved data. Keep fresh-install, upgrade and restart acceptance separate.

Keep Loom responsible for the reactive agent loop and Shuttle responsible for durable business and token work. Preserve one original operation through consent, execution and receipt recovery. Keep payment evidence pending until authorized human confirmation. Use bounded vertical slices and the [QA matrix](shuttle-qa.md); retain failures without relabeling them as successful runs.

For domain details, start with [permissions](shuttle-permissions-matrix.md), [registration and pass QA](shuttle-registration-pass-parity-plan.md), [payment contracts](shuttle-payments.md), [original order operations](shuttle-order-operation-adoption.md), [meal submission](shuttle-meal-order-submission.md), [massage schedules](shuttle-massage-schedules.md), [avatar lifecycle](shuttle-avatar-telegram-sources.md) and [knowledge](shuttle-knowledge-bases.md).
