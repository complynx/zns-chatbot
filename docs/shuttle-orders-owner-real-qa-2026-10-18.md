# Owner orders and evidence, actual Telegram — 2026-10-18

Status: PASS for owner order submission and pending payment evidence through the
agent alone, and agent preparation to native confirmation back to agent evidence
submission. Actual Telegram current account and `@complynx_test_bot` only, real
IAM/Loom/PostgreSQL and a manually answered provider. These owner workflows do
not confirm payment. Pure-agent reviewer and agent-to-native reviewer decisions
remain separate gates. No production readiness or unattended interpretation claim.

This owner workflow ran on schema48 / `mvp-54`. The later
[catalog contract image activation](shuttle-catalog-contract-install-qa-2026-10-18.md) installs the reviewed/tested `mvp-55`
contract correction and rechecks both pending claims with fresh IAM authorization.
It does not rerun or broaden this historical owner acceptance.

## Fixtures and file references

A bounded helper created two distinct synthetic events through the owned admin
APIs, each with assigned unpaid participant pass, current account's event-scoped
ambassador authority, full pricing and a four-unit soup resource. No global IAM
grant was added. Sales close 21 October; the service is on 22 October, in
Europe/Berlin. These are setup fixtures, not proof of registration/assignment UI.

The two distinct synthetic PDFs had already been accepted from this Telegram
account and were not used by any active payment claim. The manual provider used
their known fixture IDs. This does not prove automatic filename resolution from
old conversation history or autonomous receipt understanding. Actual broker reads
verified owner metadata and imported each file through `zinusya_files.fetch`:
1555 and 1565 bytes, each with its own source ID, hash and expiring Loom artifact.
The bot sent file references, and Loom requested the bytes; the harness did not
push a file body to Loom. PDF visual analysis/conversion is outside this gate.

## Entire owner workflow through the agent

The Telegram request asked for two soup portions, exact full quote before order
approval, then binding the named PDF as full-payment evidence without confirming
money. Actual Loom tools read events, catalog, empty selection, complete quote
and selected file metadata. Saving quantities alone created no order or hold.

The real approval showed two portions at 12.34 BYN each, 1.00 BYN mandatory service
charge, total 25.68 BYN, event-local service/cutoff and exact confirmation expiry.
Before consent there were zero orders, evidence rows and allocations for this
event. A free-text consent reached the same run and exact argument hash.

Submission created one order and cleared its consumed draft. There were still
zero allocations before evidence submission. After fetching the selected PDF by
ID, the real `zinusya_order_payments.submit` call bound it to that same order.
This created one pending claim and two held soup units, with no review or confirmed
money. One pending business notice delivered, even though the payer also has the
fixture event reviewer role. The final Russian reply stated that human review
was still required. No Mini App was opened for this workflow.

Run: `f62f44d0-dd1c-4fd0-b509-0c052ca2c5fa`.
Order: `b9c71339-f68e-468b-92b8-fef903659645`.
Evidence: `285dc06e-e7f1-4b91-ade9-b634bca4c0a6`.

## Agent to native form and back

A separate Telegram request saved and prepared two portions in the other event.
While that run waited for approval, `/orders` executed as a structured native
command and delivered Open saved selection. It created no new Loom run and did
not resolve the pending approval.

The actual Mini App loaded saved selection version1 with two portions and the
same total. Review order reused the exact original preparation ID, arguments,
frozen snapshot and expiry. It did not create a replacement preview or renew
consent. Native Confirm this order created one order using that preparation.
The page showed submitted, payment not confirmed and no reservation from
submission alone.

Back in Telegram, an untagged message said the form had confirmed the order,
asked not to submit it again and requested binding the other PDF. The actual
interaction resolver rejected only the pending dialogue action. A SELECT verified
that the complete native order row remained unchanged. This did not cancel it.
Subsequent real broker reads found that one submitted order, fetched the distinct
1565-byte PDF and bound it to the same order as pending evidence. Two soup units
were held. There was one submit receipt, not a second order. The final reply and
pending notice both stated that payment remained unconfirmed.

Run: `22558703-c84f-4cc2-a950-098282f5892d`.
Order: `bb989c1e-c0e3-4bdd-849f-f54f6aabd613`.
Evidence: `4fc99649-028f-4274-b501-acd6984a18c9`.

The browser's DOM selectors returned an empty surface in the embedded WebView.
Documented accessibility controls selected the observed event label and confirmed
the displayed order. A coordinate dropdown attempt was rejected by the browser;
it was not bypassed. No extra message or replacement operation repaired these UI
observations. The native result was observed before returning to the dialogue.

## Result and independent review

The read-only final gate verifies two orders, two different pending proofs, four
held soup units and zero review rows. Each run has two completed tasks and three
delivered task outboxes. The extra `/orders` task and launcher outbox are also
completed/delivered: five tasks and seven task deliveries in total, plus two
separate pending business notices. Both final replies were observed in Telegram.

The ten main containers retained IDs, images and start times and had no OOM flag.
No additional QA container was started. No bot task is unfinished. Global IAM
permissions and Loom developer stands were not changed. The new fixture event
reviewer roles are intentionally retained for the next reviewer gates.

Fresh reviews found no actionable issues in the fixture builder or final gate.
Dispatcher review found that an old answer file could be sent to a newer step;
the driver now requires the exact observed ClientRequestID, same-case filename,
new response ID and previously unused answer path. It stops on an uncertain
publication intent. Fresh follow-up review found no actionable findings.

Two read-only final assertions failed and were corrected without replaying any
business action: notices have evidence_id, not event_id; native tasks store an
empty run_id string, not SQL NULL. Both failures and their diagnoses are retained.
A save of fetched-artifact evidence first hit the filesystem sandbox; it was
repeated with workspace write permission, not by refetching or resubmitting proof.

## Incorrect source instruction fixed; runtime activation pending

The actual `mvp-54` catalog manifest still said “Order proof/review is not
implemented.” This contradicted its existing `zinusya_order_payments` module.
`orders_manifest.go` now points to that separate module, and `manifest.go` advances
the source revision to `mvp-55`. No business logic or schema changed.

Independent source review found no actionable findings. Four existing HTTP
manifest/health/file/massage contract roots passed on Go1.27.2 with no failure or
skip. Target Linux golangci-lint passed with zero issues. The initial lint failed
because Windows text writing introduced noncanonical line endings; gofmt corrected
the two files. That failed log remains retained. The final textual diff still
matches the independently reviewed diff. A new prose-mirroring test was not added.

The real owner workflows above used the installed `mvp-54` image. Their manually
supplied tool calls do not establish that the stale description is harmless for
a real model. Next: build and activate the reviewed `mvp-55` image on the owned
stand, then complete pure-agent and agent-to-native reviewer decisions for these
pending claims with fresh exact consent. Fault/outage and unattended-model gates
remain open. Earlier failed/expired review history is preserved.

Ignored evidence: `.cache/shuttle-work/order-owner-real-v1-qa/`, including
`fixtures.json`, per-case request/answer/actual-result files, `checkpoint.json`,
`manifest-source-gate.json` and retained assertion/lint failure receipts. No commit,
push, migration or production change was made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk

Subsequent [reviewer dialogue and cancellation QA](shuttle-order-reviewer-dialogue-qa-2026-10-18.md) confirms the first claim through the agent
and cancels only the second review intent after an incomplete native attempt.
The second claim remains pending. This does not change this report's historical
acceptance state or broaden its scope.
