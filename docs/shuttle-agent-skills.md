# Agent skills

Shuttle publishes task procedures through Loom's tool-scoped skills. The initial
catalog contains names and short activation conditions. Full instructions load
only when the agent needs them. Skill bodies live in
[`internal/agentskills`](../internal/agentskills).

## Core and deferred guidance

Every conversational prompt retains persona, response-language selection, input
trust, consent, current-version checks and uncertain-operation recovery. Personal
memory remains part of Loom's core loop. Skills do not replace these rules.

Deferred procedures cover names, event facts, registration, pairing, pass and
order payments, independent reviews, service and meal orders, massage, specialist
reads, avatars and knowledge maintenance. Pass and order reviews stay separate
because their schemas and financial effects differ.

Each `usecase` states only when to load the skill. Its body states the procedure,
exact deferred method IDs, domain-specific consent and recovery rules. The agent
copies both name and source from the advertised catalog when calling
`modules.skills.read({name,source})`; tool skills do not use the default
organization source.

Before calling a selected method, the agent reads its full paged contract through
`modules.help.read`. Reads use `modules.<module>.<method>` in script; effectful
service actions use `module_call`. Loading a skill
does not execute work or authorize it.

## Discovery and authority

Account, current profile, event context, knowledge search and file metadata stay
in the initial overview. Other methods remain in normal discovery lists. Name
actions covered by the name skill use direct help lookup instead of the overview
or search. Language changes and operation lookup remain discoverable.

Skills attach only to their publishing method after existing authorization
filters. Their `require` dependencies are checked by Loom. This proves method
availability, never event eligibility, private staff binding, user intent or
consent. Every effect still passes existing server checks.

Loom's builtin media, document, diagnostics and skill-library procedures are
reused. Shuttle does not duplicate them or grant skill-library write rights.

## Compatibility and verification

Skill descriptors require the Loom skills protocol. Legacy descriptors and
prompts remain the default until a deployment explicitly enables the integration.
Older strict readers reject the new fields; disable the integration before
rolling back Loom. Existing run requests remain their original durable snapshots.

Focused checks cover descriptor publication, unchanged authorization and effect
contracts, hidden-method discovery paths, legacy behavior and prompt boundaries.
Model acceptance must separately prove skill selection, full-contract lookup and
correct behavior for ambiguous consent, native handoffs and uncertain effects.
Synthetic provider checks do not establish real-model acceptance.
