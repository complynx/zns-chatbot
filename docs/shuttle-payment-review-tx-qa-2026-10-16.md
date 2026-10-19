# Payment review transaction groundwork QA, 2026-10-16

This is a behavior-preserving extraction for the next
[native reviewer slice](shuttle-native-payment-review-plan.md). It does not implement
its Mini App, shared review workspace, outer-operation bridge or four-path workflow.
The owned runtime remains on schema 38 and its previously accepted image.

## Change

ReviewPaymentEvidence retains public validation, transaction ownership and commit.
Its private transaction-bound core retains canonical operation/event/actor locking,
proposal binding, current authority and money checks, ledger revision, durable owner
notification and original receipt. Existing receipt replay returns without committing
new state, as before. A future workspace caller can use this core inside one transaction
with its own state and transport receipts. No schema or business rule changed.

## Verification

The identical Linux race command ran before and after extraction against separate,
new disposable PostgreSQL databases:

```
go test -json -race -count=1 -p=1 -parallel=2 -timeout=180s ./integration ./internal/payments ./internal/api ./internal/db ./internal/task -run '^Test(.*Payment|.*Notification|.*Authority|PassDeposit)'
```

Both runs passed the exact expected set of 40 top-level tests and 62 test pass events:
36 integration roots/53 events, two payment roots/two events, one database root/six
events and one task root/one event. The API package compiled but had no matching test
roots; this is not separate API test coverage. No failures or skipped tests occurred.
The scope includes notification and authority scenarios selected by that regex; it is
not the full rewrite test suite or real-service user acceptance.

Each gate froze 825 release/test inputs. The only changed input between gates was
internal/db/payments.go. The ten main stand containers retained their recorded IDs,
images, start times, restart counts and running state. Test containers were limited to
2 GiB and PostgreSQL to 512 MiB, without additional swap; both completed without OOM.
All four disposable containers and both owned database volumes were removed. This
was cleanup of test databases, not application graceful-restart acceptance.

A fresh independent source review found no actionable findings. Two independent plan
review passes found and closed one missing requirement: native prepare/edit/cancel and
completion metadata must reach the next explicit agent turn without starting Loom.
Scoped database lint passed with zero issues. The Git index remained empty.

Local proof: .cache/shuttle-work/payment-review-refactor/proof.json links the retained
baseline/after checkpoints and their JSON test output, expected roots and frozen inputs.
No production deployment, runtime cutover, commit, push or PR was performed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
