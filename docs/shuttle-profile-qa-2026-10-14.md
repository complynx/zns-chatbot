# Profile and name expectation QA — 2026-10-14

This accepts one bounded owner-profile slice on the disposable local stand.
It does not complete the roadmap or establish production readiness.

## Source checks and reviews

The architect and Go engineer agreed the row/receipt/CAS design before coding.
An independent correctness review found that a new Loom run had no evidence
of the previous conversation language. The fix adds up to three scoped, bounded
completed exchanges as untrusted language evidence. Fresh correctness and
quality/documentation reviewers then reported no actionable findings. Two
review rounds were used; the staging area remained empty.

The initial behavioral run found Local/UTC representation differences between
fresh PostgreSQL instants and JSON receipt replay. Profile observation and name
expectation instants now use canonical UTC. Focused profile tests then passed
with `-race` in 12.353 seconds.

The full Linux race suite passed for `cmd`, `internal`, `migrations`, and
`integration`; integration took 137.057 seconds. This full run preceded the
bounded conversation-excerpt addition. After that addition, a focused combined
run failed once because an excerpt was absent. A diagnostic single run passed,
ten successive isolated runs passed, and the final unchanged test without
diagnostic reads passed three combined runs including profile and adjacent
routing/recovery cases in 80.515 seconds. The initial observation is retained
in the ignored QA log; its cause was not reproduced or established. These
checks are not a claim of exhaustive timing coverage. Final strict lint reported
zero issues; `sqlc generate` and `git diff --check` completed successfully.

Logs and container exit/OOM states are under ignored
`.cache/shuttle-work/container-results/zinusya-profile-*`. Completed test
containers were removed. No test container reported OOM.

## Real-service acceptance

The owned stand uses real Zitadel, real Loom with the existing isolated seal
fixture, PostgreSQL and the authorized Telegram account/chat. Loom's provider
is a manual Codex mailbox: tool requests were driven explicitly. This proves
transport, scoped tool execution and durable state, not unattended model
classification or language compliance.

Current bot image:
`sha256:df06ab127ea47fbff62fd5065f8ab2b9663b34146e7b2b264a357c43bfdbadae`
(`shuttle-test-bot:profile-candidate`), schema 19, manifest `mvp-15`.
The ignored `profile-candidate.override.yaml` selects this image. Do not start
the stale generic `shuttle-test-bot:local` tag for this stand.

Before migration, `pre-profile-20261014.dump` was saved and its archive listing
validated. Its SHA-256 is
`a5c32a9d0dc2a0d9a6335510bfa7432120071ffd2b2110d1cdcae78fb798bac8`.
The prior tier image is retained as `shuttle-test-bot:pre-profile-20261014`.
The first rollout helper omitted the separate `-migrate` command; its schema
check stopped at 18 before any Telegram test message was sent. The same saved
checkpoint/backup was retained, and the normal bounded migration command was
run before restarting the candidate. Schema 19 was then verified. Existing
user/task/registration/pass/evidence/review/pair digests were unchanged.

From the existing authorized Telegram account, one test request saved English
as the interface language and requested a full name. Native Loom called
`set_language` and `request_name` successfully, without an approval workflow for
these ordinary owner changes. The bot asked for the name in Russian, matching
the meaningful message rather than the interface language.

A second message contained only the test name `Daniel QA`. The new run's input
contained the active expectation and prior Russian exchange. Native Loom called
`complete_name` with the exact version and expectation ID. The name was saved,
the expectation cleared, and a Russian reply was visibly delivered. The profile
and original completion receipt survived an actual restart of the bot alone.
An ensuing `/cancel` received the English system reply `There is no active task.`

The request run was `d6a7ddc7-7369-45fc-a1fd-c39709f18edd`. Later run, task and
operation IDs, the original completion receipt and delivery status are retained in the ignored
`native-profile-checkpoint.json`. Screenshots `profile-native-completion.png`
and `profile-native-language.png` were saved and visually inspected. Only this
authorized account and bot chat were used; no fixture Telegram account received
messages.

Direct API checks with an existing ordinary real-IAM fixture accepted all
remaining methods: repeating a live request preserved its deadline, a wrong
cancel ID rejected, a correct cancel succeeded, and an explicit name save
succeeded. An expired completion rejected without replacing the saved name.
Status returned the original request receipt even after expiry. A different
global administrator could not read that owner's receipt. Financial-table
digests were identical before and after these tests. There were zero
nonterminal Loom runs at the final check.

## Remaining boundaries

The later full avatar/profile race run reproduced the earlier intermittent missing
excerpt failure. The task's agent result was successful, but the language query
excluded delivery-gated task states. A controlled lost Telegram response reproduced
the missing context without a timing race. The query now accepts successful agent
snapshots in terminal delivery states; it does not retry their messages. Three
focused runs passed for both restart paths. A fresh correctness reviewer and a
fresh quality reviewer found no actionable issues. The subsequent full-suite result
is recorded with avatar acceptance rather than replacing the earlier failed log.

No proactive agent, generic expectation scheduler, legal-name verification or
IAM renaming was added. Payment expectations continue to derive from business
records. UI buttons/menu entry points are not added by this slice; the agent API
is the current user-facing path. The profile data is retained on the test stand
as test state. Browser CA/zitadeltg interactive acceptance still waits for the
previously agreed user session. No production deployment, merge or new release
commit is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
