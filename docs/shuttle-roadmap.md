# Shuttle roadmap

Shuttle is the standalone Go implementation of zns-chatbot / Зинуся. The [specification](shuttle-spec.md) defines the target behavior; the [architecture](shuttle-architecture.md) defines ownership. The application uses the consolidated [schema76 baseline](shuttle-schema.md).

## Current scope

The source includes identity and profile entry, durable task routing and recovery, registration and pair/pass workflows, payment evidence and human review, service orders, meals, massage, avatars, knowledge and historical account import. These features use shared native and agent business operations. Bounded acceptance exists for these paths; it does not establish every role, contention window or external-service failure combination.

[Sticker and custom emoji descriptions](shuttle-symbols.md) add active reaction input, a bounded popularity catalog and a separate visual-description worker with bounded parallelism. Optional bot reactions use exact message receipts and independent delivery recovery. New conversational runs require the Loom output-policy capability for same-run custom emoji rejection and correction. The descriptor requires Loom's native-file execution profile and a dedicated account. Full animation-description acceptance is incomplete. Controlled consumer tests do not replace actual-model acceptance.

Controlled orchestration and actual-model, browser, Telegram and provider observations retain separate scopes. A successful local path does not itself authorize release or establish production readiness. Detailed run evidence and installation configuration are maintained outside tracked product documentation.

## Remaining work

1. Complete the human real-API walkthrough, including real Telegram audio and the relevant native/agent handoffs. Reuse established evidence where the behavior and source remain applicable; do not rerun the entire matrix merely because a dependency changed.
2. Resolve failures or unmet requirements found by that walkthrough at the narrowest responsible layer, with focused behavioral proof and independent review.
3. Complete the release readiness decision: current source/CI, supported schema and account-history transition, current authority, preservation and recovery boundaries. Publication and production cutover require separate authorization.

## Working rules

Keep Loom responsible for the reactive agent loop and Shuttle responsible for durable business and token work. Preserve one original operation through consent, execution and receipt recovery. Keep payment evidence pending until authorized human confirmation. Use bounded vertical slices and the [QA matrix](shuttle-qa.md); retain failures without relabeling them as successful runs.

For domain details, start with [permissions](shuttle-permissions-matrix.md), [registration and pass QA](shuttle-registration-pass-parity-plan.md), [payment contracts](shuttle-payments.md), [original order operations](shuttle-order-operation-adoption.md), [meal submission](shuttle-meal-order-submission.md), [massage schedules](shuttle-massage-schedules.md), [avatar lifecycle](shuttle-avatar-telegram-sources.md) and [knowledge](shuttle-knowledge-bases.md).
