# Created-run ownership on reply-mode mismatch, 2026-10-21

## Defect and change

The Loom client could receive a successful creation response with a run ID but
without the requested agent reply mode. It classified this as rejection. The
task then failed before saving the returned ID, although the remote run could
continue under `disconnect_policy: continue`. This is a Shuttle client/task
defect; the probe does not establish an upstream Loom defect.

The client now reports an uncertain effect rather than rejection. When a created
ID is available, the task durably records that ID and an incompatibility marker
before cleanup. It rereads the original run and requests cancellation with a key
derived from the immutable task ID. A lost cancellation receipt does not cause a
new run. Only a fresh terminal run observation permits automatic failure of the
local task; incompatible results and prompts are not forwarded to the user.
Later controls wait while that parent is being reconciled.

Cleanup does not expire under the ordinary Loom discovery retry deadline.
Current authorization is still checked before network calls, and local rights
revocation blocks those calls. Ordinary tasks keep their existing bounded retry
policy. The existing unknown-reauthorization manual gate remains: this change
does not add unsafe retries of an uncertain authorization mutation. Explicit
operator closure remains a separate audited decision and does not claim remote
cancellation.

The checkpoint adds one optional marker; no database migration or API/manifest
change is required. A rollback to a binary unaware of that marker requires
inspection of any pending incompatible-run cleanup first.

## Verification

- The HTTP-client regression test fails on the original classification for both
  missing and protocol reply modes. It also asserts that the created ID remains
  available to the caller. The supported agent-mode case remains successful.
- Full client and task package tests pass after the change and after extracting
  error handling from the main run observer.
- Final frozen-source Linux/PostgreSQL/race QA passes 12 roots / 28 cases, with
  no failures or skips: two client roots and ten integration roots.
- The new integration scenario covers an applied cancellation with a lost
  receipt, and an unapplied cancellation with a lost receipt. In both cases it
  restarts the application and verifies one created run, the original durable
  ID, the stable cancellation key, no Input calls and eventual remote
  cancellation before local failure. The unapplied case also restores
  authorization after a restart with an already expired discovery deadline.
- Existing conversation and authorization-recovery cases remain in that gate,
  including the ordinary task's persistent-denial deadline and local revocation.
- Strict lint for client, task and integration packages reports zero issues.
  The initial complexity failure is retained; error handling was extracted
  without changing its ordering, and the same behavioral gate passed again.
- The final independent code review reports no actionable findings. An earlier
  review found the authorization-timeout custody escape; that path was corrected
  and added to the behavioral scenario before final acceptance.

## Scope and retained evidence

These checks use actual PostgreSQL and HTTP fixture peers, not real IAM, actual
Loom or Telegram. Exact committed-source remote CI and installation remain
separate gates. The owned primary is unchanged at schema69/mvp-89; production
was not changed.

The three terminal runners and their PostgreSQL, network and volume are removed
after capturing logs and fully decoding a retained database dump. No OOM was
observed. Primary domain data, queue, Loom state and runtime epochs are preserved.
Evidence is under `.cache/shuttle-work/loom-reply-mode-custody-v1/`: original red
log, intermediate and final Linux JSON events, lint attempts and retirement proof.
