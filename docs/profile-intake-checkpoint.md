# Profile name intake: acceptance scope

This slice connects the profile domain to Telegram and typed agent proposals.
It is not full festival-pass parity or multimodal intake.

## Public behavior

- `/profile` opens the current owner's profile card; `/name` or its Set full name
  button starts an unfinished name task. The button is version-bound.
- A question while the task is pending gets an answer and does not change the
  stored name or consume the task. A later supplied full name can complete it.
- A clear unsolicited own-name message may save the name without a prior command.
  Quoted or third-party names and unclear intent need clarification, not a write.
- The agent proposes a typed field update. The API supplies ownership, checks the
  same permissions/version/frozen state as manual calls, and returns the result.
  Model prose never substitutes for successful execution.
- The same profile card refreshes after a change. Old buttons and delivery retries
  must not overwrite a newer name or restart a completed request.
- `/language en` and `/language ru` localize this card and its errors. Previously
  generated model prose is a reply in the language selected for that request.

The deterministic fixture recognizes explicit `My name is Avery Example` and
`Меня зовут Иван Примеров` self-introductions; it deliberately does not pretend to
perform general NLP. Real `gpt-6-luna` probes additionally exercise bare supplied
names after interruptions. See `profile-model-testing.md`; a real-model local stand
is available for end-to-end manual acceptance.

## API and privacy

The [public API contract](profile-api.md) lists payloads, errors, replay semantics
and synthetic fixture controls for independent acceptance.

`GET /v1/me/pass-profile` reads the authenticated owner's profile.
`POST /v1/me/pass-profile/actions` accepts typed commands with name, field, value,
version, key and origin; it never accepts an owner from the body. Domain supports
role/legal_name/passport begin/submit/set/cancel operations. This Telegram slice
exposes only the name path. A frozen identity blocks name/passport changes, while
role changes retain the domain's separate rule.

The model receives profile readiness flags and the current user message, not stored
passport/name values. Raw user messages are not copied into the general interaction
records. Shared model history contains action metadata and system notices, not raw
user input or generated conversational replies: either can contain identity values
regardless of the selected view. Legacy input values are also removed when assembling
context. Replies remain available in the owner's chat. The private
execution cache retains an explicit name proposal for reliable retry; it is not
model history. Profile action/idempotency metadata and errors contain no values.
The owner's profile card legitimately displays the stored name, never passport data.

## Verification status

Focused domain/API and Telegram PostgreSQL tests pass, including interleaving,
unsolicited EN/RU names, same-card refresh, replay after a newer change, stale buttons,
frozen/denied writes, and failed-model privacy/language behavior. Go lint is enforced.
Review fixes cover history redaction, authoritative manual API history and removal
of generated profile answers after a newer profile version. Independent real-model
Functional QA also led to explicit replacement semantics for own-name introductions.
Fresh Code QA and Functional QA completed against these changes. The profile-name
slice is accepted; full pass functionality and media intake remain separate work.

The current full local quality run completed successfully on 2026-09-25: pinned
Go/JS lint and formatting, module checks, vet/build, PostgreSQL integration with
race detection, fuzzing, live sandbox checks and all nine browser suites. Static
Code QA pass 5 found no actionable issues. The vulnerability scan found no affected
code or imported packages; one advisory remains in an unused required module.
Independent real-model Functional QA pass 2 passed EN mouse and RU emulated-touch
scenarios, including interruption, bare later name, unsolicited replacement,
ambiguity, current manual history/card, permissions/frozen state, stale writes,
retry/concurrency, graceful restart and Telegram 429 recovery. See the local
[report](../qa.local/functional-profiles-pass2/report.md). Each utterance was sampled
once; this is not statistical model reliability. Crash-at-commit and model-outage
manual scenarios were not run. The optional native stand was stopped and released.
