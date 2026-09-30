# Independent Functional Senior QA B: recovery and import

Status: PLAN ONLY. Every scenario below is UNTESTED. No acceptance is granted.

Ownership: this reviewer owns `docs/qa/fqa-recovery-import-*.md` and `qa.local/fqa-recovery-import-reviewer/` evidence only. The stand engineer is the sole preparation writer until the lead hands off a frozen epoch. Recovery and import testing run serially with separate state. No product edits, Git operations, production actions, or outside messages.

Review method: black-box Telegram-like UI and documented public controls. No implementation source, diffs, conversation history, prior findings, or fix hints. Synthetic fixtures prove only their stated behavior. Real model, Zitadel, and provider behavior remain separate acceptance gates.

## Required handoff

The lead must provide all of the following before execution:

- Immutable reviewed epoch, image digests, build identity, and freeze timestamp for each stand.
- Recovery namespace `synthetic-qa-zns-fqa-recovery`: PG 58411, app 58412, fake provider 58413, control 58414. Import namespace `synthetic-qa-zns-fqa-import`: ports 58421–58424. Confirm separate volumes, credentials, queues, and fixture identities.
- Public UI URL, EN/RU roles and accounts, sandbox-only credentials, and a manifest of the starting synthetic state.
- Exact control contracts: endpoint or UI action, arguments, permitted target, expected acknowledgement, observation method, and restoration method. Controls must reject production and other reviewer namespaces.
- Sole-writer handoff and exclusive fault-window protocol. No rebuilds or stand upgrades during a frozen epoch. Release the epoch before requesting upgrades.
- Public read-only observations for accepted events, update identities, queue order, pending/uncertain/parked states, operation identities, deadlines, session ownership, process/helper lifecycle, restart/backoff, import commits/conflicts, and redacted diagnostic collection.
- Exportable fake-provider ledger including acceptance time, chat/topic lane, operation identity, result, and deliberate ambiguity. Preserve EN/RU UI evidence with enough context to reproduce each outcome.
- Resource-limit evidence for the CPU-only Linux main app plus PostgreSQL and isolated bounded helpers. Identify supervisor and process lifecycle observations without exposing secrets.
- Import fixture package and immutable hashes: source snapshot, plan bytes, resolution bytes, independently attested identity mapping, receipt bytes, owner-bound proof, permanent identity/legacy-reference expectations, and dependency-order expectations. Include a documented unavailable-receipt case.

No direct database mutation by this reviewer. If a control cannot express a required scenario, report it as unavailable and request an upgrade after releasing the epoch.

## Required recovery control capabilities

1. Hold/release receipt, persistence commit, and delivery boundaries, with evidence that the intended boundary was reached.
2. Crash or stop only this stand's app at those boundaries and observe managed restart, helper join, session ownership, and prevention of overlapping launches.
3. Cause a drain timeout while the old app/helper remains alive; cause admission-session loss during active work; cause transient database unavailability and restore it.
4. Script valid repeated 429 responses, ambiguous 429 responses, invalid delays, denied recipients, shared-credential failure, and uncertain possible-send outcomes.
5. Observe and manipulate synthetic clock/deadline boundaries through an agreed contract, or provide a bounded real-time schedule. A control must not bypass normal business state transitions.
6. Hold a selected announcement chat/topic lane while independent lanes proceed. Change opt-out and authorization through normal usable UI before deferred delivery.
7. Trigger collector failures and expose bounded redacted output without interfering with business work.

## Required import control capabilities

1. Stop runtime writers and prove they remain stopped throughout import/reconcile.
2. Stage immutable fixture bytes and independently attested identity resolutions; apply, interrupt, resume, replay, and reconcile through public admin controls.
3. Select a fixture with a late conflict after an earlier record commits; select changed-target and changed-plan/resolution fixtures. Controls must never silently repair or overwrite.
4. Remove temporary receipts through the documented lifecycle, build/start runtime without importer, and inspect preserved permanent identities, references, proofs, history, and domain state through public exports/UI.
5. Exercise receipt lookup for immutable bytes, owner mismatch, and unavailable bytes. Unavailable must be explicit.

## Recovery scenarios

Each row starts UNTESTED. A result needs UI/ledger/lifecycle evidence, fixture identities, exact fault timing, epoch, and limitations.

| ID | Scenario and smallest sufficient proof | Status |
| --- | --- | --- |
| R01 | EN and RU Telegram-like UI: send messages, use buttons/keyboards, receive callback acknowledgement, inspect edits, upload and download, complete manual and fixture-agent flow. | UNTESTED |
| R02 | Accept a full event batch; crash before/at receipt and commit boundaries. Compare acknowledged events/offset with durable work after restart. No acknowledged accepted work may disappear. | UNTESTED |
| R03 | Replay the same update identity and restart a multi-event chat. Verify deduplication and per-chat order with one domain effect per accepted identity. | UNTESTED |
| R04 | Crash at delivery boundary before known send and after possible send. Known durable work resumes; ambiguous possible sends remain explicit and are not blindly resent. | UNTESTED |
| R05 | Request managed replacement while old app and helpers are active, including drain timeout. Observe stop/join and no second launch while old processes remain alive. | UNTESTED |
| R06 | Lose admission session during active work. Verify old writers stop before replacement can write. | UNTESTED |
| R07 | Fail the database transiently. Observe app exit, genuine supervisor restart/backoff, restoration, and preserved work. | UNTESTED |
| R08 | Deliver repeated valid 429 responses and restart while deferred. Verify durable retry deadline, no ordinary failure-budget consumption, and eventual progress. | UNTESTED |
| R09 | Return invalid retry delays. Verify visible parked state, explicit reason, and no immediate retry loop. | UNTESTED |
| R10 | Return ambiguous 429 and exercise independent chat/topic lanes. Verify conservative bot/per-chat pacing and fallback while unaffected lanes proceed. | UNTESTED |
| R11 | Hold deferred, inflight, and uncertain announcement heads in separate trials. Later items in the same chat/topic lane cannot overtake; independent lanes still proceed. | UNTESTED |
| R12 | Deny one recipient in a multi-item batch. Verify only that item ends; item progress and operation identities persist across restart. | UNTESTED |
| R13 | While send is deferred, opt out or revoke authorization through UI. Verify live permission check prevents later send. | UNTESTED |
| R14 | Fail shared credentials. Verify service-wide pause is visible and does not misclassify independent recipients as denied. | UNTESTED |
| R15 | Initiate registrations for specific events in a known order; repeat clicks and replay identities. Verify initiation rank, preserved deadline/order, and stable identity. | UNTESTED |
| R16 | Leave an intent unfinished to the default ten-minute expiry boundary. Verify unfinished intent moves to tail, draft survives, and subsequent replay retains identity. | UNTESTED |
| R17 | Fail diagnostic collector while business work proceeds. Verify collector failure cannot block work and observations remain bounded and redacted. | UNTESTED |
| R18 | Inspect resource/process observations across the preceding scenarios. Verify one CPU-only Linux main app plus PG and isolated bounded helper behavior; report measurement limits. | UNTESTED |

## Import scenarios

Import starts only after recovery releases its stand. All fixtures remain synthetic and must retain the actual source package bytes used for the trial.

| ID | Scenario and smallest sufficient proof | Status |
| --- | --- | --- |
| I01 | Stage snapshot, explicit independently attested identity mapping, plan, and resolution. Compare published immutable hashes before apply; unknown mappings cannot be inferred. | UNTESTED |
| I02 | With runtime writers stopped, apply dependency-ordered fixture; inspect identities, legacy references, proofs, history, and domain state through public exports/UI. | UNTESTED |
| I03 | Interrupt after a documented committed record and resume with identical plan/resolution bytes. Earlier commit survives and replay produces no duplicate effects. | UNTESTED |
| I04 | Resume with changed plan or resolution bytes. Verify fail-closed outcome and preserved existing target state. | UNTESTED |
| I05 | Apply a fixture with later target conflict or changed target state. Earlier commits survive; conflicting record is atomic and cannot overwrite/repair target. | UNTESTED |
| I06 | Attempt import/reconcile with a runtime writer present. Verify the public workflow prevents unsafe overlap; then run reconcile while writers remain stopped. | UNTESTED |
| I07 | Resolve receipt bytes with valid owner-bound proof, wrong owner, and unavailable receipt. Verify immutable bytes, owner enforcement, and explicit unavailable result without empty substitute. | UNTESTED |
| I08 | Complete reconcile and remove temporary receipts. Verify permanent identities, legacy references, proofs, history, and domain state remain. | UNTESTED |
| I09 | Build/restart runtime without importer, then use EN/RU Telegram-like UI on imported records. Verify persisted state remains usable and importer is absent from runtime lifecycle evidence. | UNTESTED |

## Evidence and verdict rules

- Record PASS, FAIL, UNAVAILABLE, or BLOCKED separately. Untested or skipped work cannot pass.
- Use per-scenario evidence names under `qa.local/fqa-recovery-import-reviewer/`; retain only bounded redacted logs, screenshots, ledgers, hashes, and observations needed to support the verdict.
- Separate observed result from inference. A control acknowledgement alone cannot prove the business outcome.
- A verified defect is reported with expected behavior, actual behavior, minimal reproduction, evidence, and impact. No implementation hints are requested or accepted.
- A rebuilt or upgraded stand ends the frozen epoch. Review affected substantive changes on a newly handed-off epoch with fresh evidence.
- Final report states exact accepted scope, all unavailable controls/limitations, and remaining real-model/Zitadel/provider gates. Full migration acceptance requires both independent QA gates and required checks; this plan grants none.
