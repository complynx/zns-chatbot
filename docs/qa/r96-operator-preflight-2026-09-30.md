# R96 operator preflight

This is developer/operator evidence, not independent Functional QA. Only the isolated `synthetic-qa-zns-r96-receipt` stand was changed. Product source and pinned images were not changed.

## Allocator setup correction

The initial seed inserted Alice as waitlisted with available capacity. The running maintenance worker assigned all 14 bookings before any scenario input. Live evidence: `qa.local/r96-functional-20260930/allocator-live-before.json`. Assigned participants cannot be invited through this path. These events and their notifications were preserved; they are not successful invitation evidence.

A separate additive `r96-op-queue` event has zero capacity, Alice waitlist version 1, and Bob cancelled version 1. The authorized queue includes both records. A real invitation changes Bob's record and invalidates its earlier privileged queue witness. This reproduces source retirement without relying on maintenance timing. It differs from the original integration fixture's Alice allocation witness; the difference is explicit.

## Browser input and original result

Bob selected English through `/language` and its actual button. With only the app stopped, the browser submitted `Invite Alice from the authorized queue for r96-op-queue.` The fake's directly stored pending Update supplied actual update 18; no update ID or payload was fabricated. The fixture was installed for that owner/update before app processing resumed. A separately installed inbox observer captured the first real durable insert and confirmed exact JSONB equality. Digest label: `8cf33bb4590083a9c0b30fc7a64b2545`.

The operation committed once. Alice stayed waitlist version 1; Bob became waiting-for-couple version 2, invitee 101. The saved turn became ready `agent.unavailable`, and its script was redacted. Fixture counters: accepted 1, rejected 0, next_turn 1 of 2. The browser displayed `Invitation — committed.` and `Other script steps are not confirmed.` No partner acceptance was claimed. The view also contains the existing unrelated order-selection status and buttons; presentation acceptance remains for the independent reviewer.

Evidence directory: `qa.local/r96-functional-20260930/allocator-preflight/`, including pending original payload, fixture installation, inbox match, original-proof.json, model counters, browser state, and screenshots.

## Exact replay and restart

After delivery settled, the app was stopped and the observer's exact consumed update 18 was reinserted once, with all guards satisfied. No cursor, receipt, booking, retry row, or fake counter was reset. The same app container restarted and consumed the replay.

The first full snapshot verifier failed. Its original failure and differences are preserved in `verify-replay.log`, `replay-diff.log`, and `replay-wire-diff.log`. Turn, ledger, operations, bookings, ingress, intents, intent requests, role and cursors stayed equal. Fake Messages, Edits, Next and Updates stayed equal. Model counters stayed equal. Only the menu audit changed: startup reapplied the same menu and command catalog through three successful calls; total_calls rose from 6 to 9. A full fake-state equality claim would be false.

The separate exact-startup verifier passed at 15:55:08 UTC (`verify-restart-replay-2.log`). It constructs the expected complete snapshot by appending exactly those three successful menu calls, with the documented 64-entry retention rule, then compares the entire actual snapshot. Menu payloads and all other fields must remain equal. The initial attempt of this helper rolled back because its reporting SELECT needed parentheses around JSON operators; its failed log is preserved. The original full-equality failure remains recorded and is not relabelled PASS.

Use direct `docker start` on the named app container for later replay controls. The earlier `compose start app` also started its exited migration and fixture dependencies; that occurred before the original input's evidence baseline, not during its exact replay. Direct container restart preserves the intended control scope.

## Remaining

The second additive operator event is `r96-op-revoke`, actual browser update 23. Its first timing attempt missed the intended boundary: startup maintenance produced an Alice waitlist notification, and that notification consumed the one-shot 429. Observed shared queue: chat 101, passes owner key 21, lane 17, not-before 15:58:30 UTC. At 15:58:15 UTC the input was still in the fake pending snapshot, no inbox capture or operation existed, and the fixture had zero calls. No role was withdrawn. Preserve this as a setup limitation, not a revoked-receipt PASS. Startup notifications must settle before the timed input.

After the natural deadline, update 23 was captured at 15:58:30 UTC and committed once. Its fixture consumed one proposal with no rejection; delivery settled. The missed timing case did not revoke any role and does not prove the negative authority boundary.

Fresh additive EN/RU scenarios were installed under `preparation-v2/`. At 16:02:12 UTC all 14 Alice waitlist notifications were delivered, all 14 Bob records remained cancelled version 1 and all 14 Alice records waitlist version 1. Inbox, unsettled delivery and live cooldown counts were zero; Bob's original grant remained present. All 1991 captured build-source hashes were unchanged.

Fresh source-blind reviewer `functional_r96_01` received only the public acceptance/access packet and owns its separate report/evidence. Ordinary scenarios start first. Delayed-rights acceptance remains conditional on the actual observed committed/unattempted boundary; a missed control stays unproven. This permits independent work to proceed without treating the operator preflight as passed. Existing R71/R64 stands remain unchanged. Production remains NO-GO.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
