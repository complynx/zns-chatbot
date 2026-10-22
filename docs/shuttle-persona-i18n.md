# Zinusya identity, language and visible voice

This implements the source portion of
[issue24](https://github.com/complynx/zns-chatbot/issues/24). Real-model judgment
and installation acceptance remain separate.

## Contract

Zinusya / ЗиНуСя is the female assistant for the Zouk Non Stop community and
events. Her user-visible answers, questions and generated button labels are warm,
short and concrete, with light playfulness and occasional appropriate emoji.
Russian replies use informal singular address and feminine first-person forms.
The lightly blonde persona describes conversation only. Facts, calculations,
permissions, deadlines, operation results and exact consent remain rigorous.
No age, biography or unsupported capability is invented.

The application adds one identity/style/glossary block per input, selected from
Russian and English using the existing interface-language preference; unsupported
prompt locales fall back to English. Visible response language still follows
the meaningful message/caption, then conversation language, then interface
language. No new language classifier or preference mutation is introduced.

Russian application instructions cover name onboarding, payment expectations,
pair invitations and profile continuation. Method/module IDs, JSON fields, enum
values, exact quotations and stored context identifiers remain unchanged.
The glossary distinguishes a participation application, pass, order, payment
evidence awaiting human review and confirmed payment. It does not turn an
uploaded file into confirmation or infer consent from a profile hint.

Native start/menu introduces Zinusya and retains the command list. Native errors,
name and registration messages use the same concise informal voice. Approval
buttons remain clear; callbacks, exact financial terms, amounts, deadlines and
operation/delivery identities are unchanged. Neutral form labels and factual
financial summaries do not need decorative wording. Existing Mini App layouts
and model-generated question labels retain their respective rendering paths.

## Context reduction boundary

The manifest retains its existing root identity/domain description. Method
descriptions omit the repeated180-character identity prefix, preserving all
method-specific descriptions and contracts. A recursive before/after comparison
of static, administrative, registration-details and knowledge manifests found
132 prefix-only changes and no other JSON differences:23760 characters /
24552 UTF-8 bytes across that complete captured set. An individual request can
contain fewer methods; this is not a measured token or price reduction.

The earlier actual first-contact request used30520 input tokens. Its tool section
occupied93.7% of compact JSON bytes, and Loom rendered the application input twice.
[Loom29](https://github.com/complynx/loom/issues/29) separately owns on-demand
contract loading and current-input rendering. Published Loom `5c25e0ec` / image
`bc0a7837` now passes an isolated native-request capture with this application
candidate:146981→56079 bytes (61.85%), all125 method IDs preserved, one input
message instead of two, unchanged instructions and API settings. The full
synthetic request and comparison remain private local artifacts. This does not
measure live tokens, total workflow cost or extra discovery/model turns.

## Verification and remaining acceptance

Focused tests cover one selected persona locale, English fallback, identity once
per application input, preserved JSON profile/hints and no implicit name adoption.
Native start/menu tests retain all commands in both languages. Ten focused
PostgreSQL integration roots pass, including prompt freezing across restart,
question/steering context, language fallback/notifications and revoked delivery.
The first local run exceeded the temporary database lock budget at default test
parallelism and found two stale copy assertions. Those assertions were updated;
the affected gate passed with two parallel tests. Existing API tests
and the recursive manifest comparison preserve method sets, schemas, permissions,
approval metadata, examples and operation contracts. Fresh independent reviews
of the application prompt/native changes and manifest changes are clean.

Model style is an instruction until observed with the actual target model.
Daniel selected `gpt-6-luna / low` and authorized20 additional generations,
including retries and summaries. The campaign has spent0. Automatic approval
review rejected the first paid dispatch before execution because exact payload
and destination authorization were not established. A separately reviewed
capture-only mode has no upstream client or API-key access and hard-denies
outbound connections. Both the earlier and new published-Loom captures completed
with zero external dispatches and no API-key access. Their service runs were
cancelled through the canonical owner API; original accounting is preserved and
the paid provider remains stopped. Bind the subsequent model dialogue to the
current application/runtime images. Do not reuse an old
image's successful packaging or model result as acceptance of this voice.

Required model observations include concise useful replies, questions/choices,
typed name correction, cancellation, mixed language, missing information and
honest failure. Native and financial confirmation terms remain separately
checkable without spending the model budget. Do not close issue24 from source
tests alone.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
