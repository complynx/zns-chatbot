# Agent-to-browser order continuation, 2026-10-22

The current isolated candidate has runtime evidence for an agent preparation
submitted through the shipped browser form, protection of a newer saved draft,
and completed-operation link recovery. The final continuation passed with one
submitted order, its version unchanged at 1, and the newer draft unchanged at 4.
Earlier harness failures remain failures; the evidence was continued without
repeating their business actions.

## Scope and environment

The owned `iam420-zinusya-v1` fixture used bot source `be55b4a1`, image
`7e0fc10b`, `mvp-92/schema72`, real Zitadel 4.20.0 and published Loom image
`9ddd3175`. The existing synthetic participant already had filled profile names.
A different synthetic fixture administrator created the event/catalog and
assigned the participant an unpaid pass. No personal Telegram account or
production IAM permissions changed.

Headless Microsoft Edge loaded the actual served HTML/JavaScript; hashes of
`index.html` and `orders.js` matched the source. Only the Telegram SDK was replaced
with signed synthetic initialization data. The disposable browser context relaxed
TLS verification solely for this mock fixture; it did not install a CA or change
the user's browser. This is not real Telegram WebView or certificate acceptance.
All model replies were controlled mailbox responses, with no paid model calls.

## Verified behaviors

1. The participant saved two portions at 12.34 BYN each. Real Loom prepared the
   exact order. An authenticated browser opened that original operation link and
   displayed the complete frozen event, service times, IANA timezone, quantities,
   unit prices, amounts, total, cutoff and preparation expiry before confirmation.
   One explicit UI click sent one submit request using the original operation,
   proposal and versions. Its returned snapshot equaled the original preparation
   snapshot; total was 24.68 BYN. The consumed draft became empty version 2.
2. During the harness investigation, the original agent question expired. Its
   run ended with `wait_expired`; no deadline was extended. A later explicit
   follow-up created a different run. The controlled agent read the exact original
   submit receipt through `script` and completed that read-only dialogue without
   creating another write operation. This proves post-expiry receipt continuation,
   not settlement of a still-live original question after browser completion.
3. A second agent preparation described one portion at 12.34 BYN from draft 3.
   A later native save changed the current draft to three portions, version 4,
   historical total 37.02 BYN. The browser showed that newer selection alongside
   the original one-portion preparation and disabled confirmation. The preparation
   still had more than 30 seconds of validity at this check: expiry could not
   explain the disabled button. That browser dispatched no order write.
4. A plain-text cancellation rejected only the unfinished agent question. The
   persisted original Loom operation was `rejected`. The final model response
   completed the same run; it did not invoke order cancellation or resolve the
   question again.
5. A fresh browser opened the first completed operation link. It displayed the
   original order receipt while retaining draft 4 and quantity 3. Apart from session
   bootstrap, its POSTs were the known read-only knowledge startup calls
   `overview`, `capabilities` and `list`. No prepare or submit was sent. Current
   database readback confirmed exactly one order, state `submitted`, version 1,
   `cancelled_at=NULL`, zero payment evidence and an identical newer draft.

The three browser contexts were closed. Screenshots of the stale preparation and
completed link were visually inspected: current selection, original preparation
and submitted order are separately labeled; the stale confirmation is disabled.
All nine fixture containers ended stopped with exit 0 and no OOM, and the original
bot/Loom configuration bytes were preserved. No primary or developer stand was
changed.

## Retained failures and continuation evidence

The first browser completed submission but its harness then failed because it
classified every POST as a write and blocked the unrelated read-only knowledge
overview request. After allowing overview, the stale browser proved the targeted
stale-draft behavior but the harness similarly blocked capabilities. Both browser
reports still have `passed:false`; neither is rewritten as a clean browser run.
The final completed-link browser allowed all three startup reads identified in
the actual knowledge UI source and passed with no blocked requests or page errors.

Another continuation initially used `module_call` for a read. Actual Loom returned
`use_script_for_reads`; the same follow-up was continued through `script`, without
resending its user request. Finally, Loom reported the explicit rejection as
`status:rejected` with the generic `error:operation_failed` marker. A harness that
required no error field stopped. The next continuation checked the exact recorded
operation ID and persisted rejected state, then sent only the outstanding final
model answer. It did not reject or submit again.

Every controller or correction received independent textual review before
execution. Review also strengthened full frozen-term/snapshot comparison and the
unexpired stale-draft assertion. The final review found no actionable findings.
Original receipts, source, browser journals/screenshots and failure diagnostics
remain under `.cache/shuttle-work/orders-reverse-browser-20261022-v1/`.
The final `passed_stopped` receipt and current-order readback are in
`continuation/r2/r3/r4/checkpoint.json`; its `browser-completed.json` records the
clean read-only browser result. Parent receipts are preserved and hash-linked.

## Remaining acceptance

The separate [native-to-Loom gate](shuttle-order-operation-adoption.md#native-preparation-to-actual-loom-accepted-isolated-gate)
already covers adoption with distinct Loom/backend IDs and graceful bot restart.
This reverse gate adds current-image browser evidence, but does not complete all
mixed-flow cases. Still-live pending-question reconciliation after external form
submission, other contention/access/expiry windows, real Telegram WebView and
autonomous model interpretation remain separate. This is not production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
