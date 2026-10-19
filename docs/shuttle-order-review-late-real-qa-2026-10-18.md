# Late order review, native form to agent — 2026-10-18

Status: PASS for one fresh native-to-agent review of an existing timely order
proof after its evidence deadline. Owned `shuttle-live-test`, schema48 / `mvp-54`,
actual Telegram current account and `@complynx_test_bot` only, real IAM/Loom,
manually driven provider. No real money, new product source, image change or
production action. Pure-agent and agent-to-native order-evidence flows remain open.

## Preserved failed attempt and new intent

The [earlier mixed attempt](shuttle-order-mixed-context-qa-2026-10-17.md) remains
failed. Its task is cancelled, its workspace revision1 remains prepared with an
expired proposal, and it had not produced a review. That historical failure is
not relabelled PASS. The original synthetic order still had pending proof and
two held soup resource units.

The existing event-authorized reviewer opened the real Mini App. It displayed
25.68 BYN, proof sent 17 October at 11:57:20, and an evidence deadline of 17 October
at 17:28:09, both Europe/Berlin GMT+02:00. The expired decision's confirmation
button was disabled. Load proof exposed the protected original PDF download.
This gate verifies successful authenticated loading, not a new PDF visual audit.

A new reason, `SYNTHETIC late review via dialogue; no real money`, prepared
revision2. Its new decision expiry was 18 October at 04:53:10 GMT+02:00. Saving
that revision alone left the evidence pending and its original resource hold
unchanged. The old revision1 row and preparation document remained unchanged.

## Actual dialogue and effect

One ordinary Telegram request explicitly asked to continue the new native
revision, discard the old expired consent and ask for fresh confirmation. Actual
Loom broker reads returned the current workspace and evidence under the reviewer's
user token. Its `review_commit` preparation retained exact workspace/revision,
full decision/reason, canonical literal description and original revision2 expiry.

Telegram displayed the full saved approval: event/payer/order, 25.68 BYN reported,
frozen and confirmable amounts, two soup portions at 12.34 BYN each, 1.00 BYN
required service charge, original proof/deadline, new decision expiry and two
held soup units. A new free-text consent confirmed those exact terms. Before
resolving it, SELECTs still showed zero reviews, pending evidence and held resources.

One canonical review confirmed 2568 minor BYN units, using the current event
reviewer's identity and new reason. Only the original soup allocations changed
from held to occupied; their IDs, event/evidence association and units were
preserved. Evidence changed pending/version1 to confirmed/version2; all other
claim fields were unchanged. The review timestamp follows the actual fresh
steering-input receipt. The original proof sent_at is before due_at, while this
review timestamp is after due_at: late human review did not reject timely proof.

Run: `61a41d7f-76d0-4dab-a1e7-e7a23b456352`.
Canonical workspace: `2df0e9de-9fbc-45a2-b5fa-f12f20362661`, revision2.
Evidence: `78d18974-3e24-4268-a57a-86ccf6cb54f4`.

The transport commit has its own proposal ID, as defined by
`internal/db/order_review_commit.go`. It is distinct from the native canonical
proposal, but retains identical full arguments, description and expiry. The
agent operation resolves to the same canonical workspace. A fresh IAM-authorized
status read recovered that one result after the run finished; no replay write
was sent. Exactly one canonical review exists for this event.

## Delivery and native result

Both bot tasks completed. Three task outboxes delivered approval, steering
acknowledgement and final reply. A separate reviewed-payment notice also delivered
once. The claim has two delivered business notices in total: its earlier pending
notice and this new reviewed notice. These are not two new review notifications.

The final Russian reply and the reviewed notice were observed in Telegram.
Opening that actual notice's Review order proof button selected the same evidence
in Mini App, showing Payment confirmed, the same amount/reason and Decision
recorded. Prepare/confirm/cancel controls were disabled for the completed decision.
The order remains submitted/version1: this confirms payment, not fulfillment or
an executed refund.

No temporary IAM grants were added. The ten owned Compose containers retained
the same IDs/images/start times and had no OOM flag. Both nine-service mock stands
remain stopped. No QA containers were created and no bot task is unfinished.

## Review, retained QA failures and limits

A fresh helper review found that checking only occupied-unit totals could miss
a resource substitution. The final driver compares every allocation field with
the baseline, allowing only held to occupied. A second independent review found
no actionable findings. The index remains empty; no commit or push was made.

An extra final check initially assumed native and transport proposal UUIDs must
match. That assertion failed. Source confirms these UUIDs are deliberately
distinct; corrected read-only checks matched exact parameters, description,
expiry and canonical result. The original failure and diagnosis are retained;
no review was repeated to repair the QA assertion.

Telegram browser observations also timed out after sending the one initial
request and when matching its long code-formatted approval. The existing task and
run were observed instead of resending. The current accessibility tree exposed
the complete terms and confirmed receipt of the one fresh consent. After opening
the reviewed notice, Playwright locators temporarily returned an empty surface;
the documented accessibility controls and screenshot verified the same completed
native result. These observation failures are not product defects.

Ignored evidence: `.cache/shuttle-work/order-review-late-real-v1-qa/`:
`before.json`, `run.json`, `checkpoint.json`,
`final-transport-assertion-failure.json`. The earlier failure and cancelled run
remain separately retained in the 17 October report and proof directory.

This accepts this native-to-agent review and timely-proof/late-review critical
path with manual inference. It does not accept pure-agent owner evidence intake,
the complete agent-to-native order path, source-file loss during review, upstream
outages, unattended model decisions or production readiness. Those gates remain
separate. The earlier counted-provider activation remains configuration evidence;
this gate additionally establishes an actual Loom-originating request and tool
cycle through that manually answered provider.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
