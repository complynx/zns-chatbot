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
`bc0a7837` now passes an isolated native-request capture with application
candidate `498b6db8` / image `bd8cc621`:146981→56079 bytes (61.85%), all125 method IDs preserved, one input
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
including retries and summaries. At the A4 checkpoint the campaign had spent9, leaving11. Automatic approval
review rejected the first paid dispatch before execution because exact payload
and destination authorization were not established. A separately reviewed
capture-only mode has no upstream client or API-key access and hard-denies
outbound connections. Both the earlier and new published-Loom captures completed
with zero external dispatches and no API-key access. Their service runs were
cancelled through the canonical owner API; original accounting is preserved.
Paid-provider restoration and model dispatch are separate reviewed steps. Bind the subsequent model dialogue to the
current application/runtime images. Daniel subsequently explicitly authorized
the displayed payload and then updated synthetic test prompts, instruction
corrections and dialogue continuations to the same OpenAI destination within the
unchanged total20 allowance. Do not reuse an old
image's successful packaging or model result as acceptance of this voice.

The three initial real Luna calls returned200 but did not produce a delivered question
or confirmed name. A1 attempted `request_name` through read-only JavaScript; the
backend correctly denied it. A2 sought help and the profile but supplied numeric
`cursor:0`, while the help contract requires a string. A3 read the complete help
card successfully but called the read-only `get` through `module_call`, receiving
`use_script_for_reads`. The original test proxy conservatively froze before
another generation whenever a tool result contained an error.
The failed runs were cancelled through the owner API; original unknown steps,
credits, real usage and frozen campaign copies remain preserved. The remaining
allowance at that checkpoint was17; there is no reset or refund.

The localized operational instruction now explains selected full help, current
profile version, durable `module_call` routing, first-page help without cursor and
unchanged string `next_cursor` for later pages. Focused locale/prompt tests, lint
and independent review pass. This is a source correction, not successful model
dialogue or voice acceptance.

Independent review identified the blanket tool-error freeze as a test-proxy
policy that differs from normal Loom recovery. A separate minimal counter
revision now forwards actual tool errors unchanged to the next model turn.
Three focused offline checks prove unchanged history forwarding, cumulative
accounting, per-case limits and the unchanged global20 ceiling. Backend permissions, schemas, exact consent and durable-operation recovery
remain authoritative; the proxy retains the global20-request ceiling and its
unknown paid-HTTP fencing. Earlier failed campaigns and usage are not relabelled.

The subsequent A4 dialogue used6 real generations on application source
`ca04fd3c` / image `0538c240` with published Loom `5c25e0ec` / `bc0a7837`.
The model read full help and the current profile, requested a name, and delivered
an actual question. Names remained empty until explicit typed correction.
An actual graceful bot restart preserved the expectation and its deadline.
The original restart observer failed while JSON-parsing plaintext readiness;
a separate read-only observer accepted the already performed restart without
replaying it. Both results remain recorded.

The explicit correction saved first name `Александр Павел` and last name
`ван дер Берг`, cleared the expectation, and converged local/IAM versions to2.
The delivered response used feminine `сохранила`. Read-only semantic comparison
preserved username, email/verification, provider links and grants. The first
comparison helper conservatively returned unknown for omitted zero protobuf
fields; a separately reviewed HTTP200-aware helper retained raw replies and
accepted their canonical zero defaults. Original unknown evidence is retained.
All6 A4 calls completed200, with70639 input and256 output tokens reported across
the dialogue. This is one measured workflow, without a paired old-Loom baseline.

A4 still fails the suggested-name keyboard requirement: the model delivered a
plaintext final answer rather than native `question`. Source instructions now
explicitly bind `text`, `allow_text:true` and `choices` to that native tool;
focused locale checks, lint and independent source review pass. This new
instruction is not yet model-accepted or installed in production. Suggested-name
choice and cancellation remain separate pending model gates; broad voice
acceptance and issue24 closure remain open.

The subsequent C dialogue on `057acdb2` / image `f98baa50` and Loom `5c25e0ec`
used6 generations. Actual native `question` supplied an ordinary reply keyboard
with `Александр Иванов` and free-text fallback. Names remained empty before the
exact offered choice. `complete_name` then saved separate components, cleared the
expectation and converged local/IAM versions to2; account fields were preserved.
The final feminine response used one appropriate emoji and removed the keyboard.
This accepts the current native-question/choice slice; it does not relabel old
A4's missing-keyboard failure.

Daniel explicitly extended the cumulative generation allowance20→21 by one.
B then used2 generations and stopped before any question: the model called
`request_name` without help/get or required `expected_version`. Loom rejected
the arguments locally before action HTTP dispatch, but its failure adapter
reported `dependency_unavailable`; the model described a service outage.
No outage HTTP503 was observed. No name expectation or profile mutation exists
for B. Cancellation remains unaccepted. The cumulative spend is17/21, leaving4;
all failed and successful observations remain preserved.

## Hierarchical discovery protocol update

Published Loom `70e2c5eb` (PR31) replaces the eager flat method index with a
bounded service/category overview, global/hierarchical discovery and selected
full contracts. Production read-only metadata confirms this revision across API,
sandbox, proxy and file worker; API is healthy. There is no SQL migration or
context-cache format change from `5c25e0ec`.

New application run requests explicitly persist
`discovery: {"expand_services":["zinusya"]}` in the frozen create checkpoint.
These are service IDs; `zinusya` contains our application modules. Expansion is
a bounded discovery preference, not full eager schemas or additional authority.
Selected methods still require full help and current permission/consent checks.
The generic transport adds no default; existing frozen requests retain their
original absent field and idempotent replay payload. Focused lost-receipt wire
coverage checks the new field and exact replay identity.

Upgrade Loom to70e2c5eb or newer before new app runs send this field. Before
rolling back to older strict readers, stop new discovery requests and drain
pending creates; do not rewrite frozen idempotent payloads. The application
change still needs an exact-image zero-upstream native capture. No paid-model
acceptance of this new protocol or workflow-cost saving is asserted.

Required model observations include concise useful replies, questions/choices,
typed name correction, cancellation, mixed language, missing information and
honest failure. Native and financial confirmation terms remain separately
checkable without spending the model budget. Do not close issue24 from source
tests alone.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
