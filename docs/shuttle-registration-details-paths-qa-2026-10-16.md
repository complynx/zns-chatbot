# Solo registration prerequisites: four runtime paths, 2026-10-16

Accepted scope: one solo leader waitlist request for an event requiring legal name
and passport number. Current owned bot image is ebd1bdf, Shuttle schema37/Loom4.
Actual Telegram transport, real Zitadel, the real Loom orchestration/broker and
shared business state were exercised. Provider responses were supplied manually.
The form API used locally signed proof. This is not unattended model or trusted
Telegram WebView acceptance, and it does not accept all registration/pass workflows.

## Observed paths

| Path | Telegram tasks | Loom runs | Steering inputs | Field entry and final join |
| --- | ---: | ---: | ---: | --- |
| Commands/buttons and form API | 4 | 0 | 0 | Form API, then explicit role button, while Loom stopped |
| Menu to agent | 3 | 1 | 0 | Native expectation, explicit chat values, agent join |
| Agent to form/menu and back | 4 | 1 | 1 | Agent expectation, form API, role button, same-run readback |
| Agent only | 1 | 1 | 0 | Explicit chat values and join request, native broker writes |

The first row is the earlier [native runtime proof](shuttle-registration-details-native-qa-2026-10-16.md).
Each remaining row uses a separate new synthetic event and begins with missing
fields. The three new cases produced three completed runs, eight completed inbound
tasks and one steering input. Together the four scoped cases created exactly one
waitlist registration per event, with no pass or payment.

Menu to agent: the role button created a durable pending event expectation and
no registration. The following explicit Telegram message supplied synthetic legal
values and requested leader registration. The run saw the expectation ID, resolved
the explicit event, checked readiness, saved both fields through native module_call,
read the completed expectation and joined only after the current request.

Agent to native: the explicit request asked the agent to prepare fields but defer
joining to the menu. The run resolved the event, opened a durable expectation and
asked a plain question with an ordinary reply-keyboard suggestion. Form API writes
fulfilled that same expectation; exact repeats reused the original operation IDs.
They caused no new Loom run/input or registration. /register and its role button
then created the waitlist entry. A plain text reply resumed the original run as
steering, with a persisted input receipt and no text tag used for routing. It received
redacted form action IDs, rechecked readiness and read the existing registration.
It did not issue another join. The final answer was visibly delivered in Telegram.

Agent only: the current message explicitly provided the synthetic fields, event ID
and leader join request. One run resolved and checked the event, opened the data
expectation, saved both fields, rechecked readiness/completion and created the
waitlist request. No commands, buttons or form API writes were used in this case.
All three final answers were visibly present in the authorized bot chat.

## Fixture safety and preservation

Only the authorized current account in @complynx_test_bot was used. Each setup
refused to clear fields unless both existing values decrypted in memory with the
SYNTHETIC prefix. Original encrypted rows were backed up privately. Exact-old-state
conditional fixture updates increased the version; no receipt or journal history
was deleted. These are fixture operations, not a product clear-data endpoint.

Version progression was6 ->7 empty ->8/9 for menu-to-agent,9 ->10 empty ->11/12
for agent-to-native, and12 ->13 empty ->14/15 for agent-only. Form values were not
present in any observed provider request, including compaction/summary requests.
Chat-provided synthetic values necessarily remain in Telegram/Loom history; no
claim is made that chat submissions are private from those systems.

Protected baselines were captured before fixture setup, excluding only the new
fixture event/configuration and the expected owner/event registration pair. They
cover events/rules/deposit policies, profiles, passes, pairs, payment evidence and
reviews, settings, unrelated registrations (including other owners in a fixture
event), and other owners' private details/request/receipt/journal rows. Existing
owner receipt and journal row hashes were retained as an immutable subset.

A final whole-campaign comparison excluded only the three new events/configurations
and their expected owner registrations. All 16 protected hash groups matched the
pre-campaign baseline. Original encrypted backups matched their saved hashes, and
all old receipt/journal rows remained identical. Final dedicated details storage,
receipts and manual-action journal contained no plaintext synthetic field strings.
All three QA Mini App sessions were logged out and subsequently returned401.

The bot, poller and all eight companions retained their exact fingerprints across
the three cases. No container was created, stopped or restarted for them. The
previous native-only case intentionally stopped/restored its own Loom, as recorded
in that separate proof. Synthetic events, registrations, encrypted fields and
redacted history remain in the disposable stand. No production, commit or push.

## Evidence and review

Ignored root: .cache/shuttle-live-test/event-details-paths-qa/.
Each native_agent, agent_native and agent_only directory has an accepted
checkpoint.json with run/task IDs, native tool output hashes and redacted results,
operation IDs, before-fixture business baselines and historical receipt hashes.
summary.json binds those exact accepted checkpoint bytes and records the final
whole-campaign checks and session logout. Private session credentials and encrypted
backups must not be published.

The helper is .cache/shuttle-work/event_details_paths_qa.py. Independent review
found two evidence-scope gaps: excluding other owners inside a fixture event and
capturing the baseline after fixture creation. Both were corrected before any new
case mutation. The fresh complete review found no actionable findings. No product
source was changed for these three checks; 712 release inputs still match the
installed image and accepted 812-input component gate.

Remaining acceptance includes trusted Telegram WebView entry after CA installation,
unattended model decisions, role variants, pair/admin/pass-assignment/payment
interfaces, and their four paths. Existing component restart/stale/deadline checks
remain separate evidence; these three cases did not fault-inject outages or claim
new recovery coverage. This closes the scoped solo prerequisite path matrix only.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
