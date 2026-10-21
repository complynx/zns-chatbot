# Controlled-agent meal payment acceptance, 2026-10-21

Status: bounded actual Loom/IAM/API evidence and review acceptance passes on the
owned meal-agent-pay-r1 stand. Its nine services, networks and volumes are removed
after both database exports are fully decoded. The primary remains69/mvp-89.

Bot image: sha256:85acc5a4c5e488947ce1e633eb6c301c88545be26a019efdf6ef036ffc608d69,
the same image as the [accepted installation](shuttle-meal-primary-qa-2026-10-21.md).
Loom uses the previously accepted local image
sha256:a75493893b4ded4a679679ac04ad32b5e903a3b946b53c208554e33ac90095e7.
No Loom developer checkout or stand is changed.

## Scope

Three synthetic real-IAM identities and one event with an assigned unpaid pass
are configured. One665minor BYN meal order is created through direct API calls
as setup. That setup is not a browser or agent acceptance claim.

The subsequent dialogues use actual Loom and bot APIs with a controlled mailbox
provider choosing the tool calls. The photo is a synthetic68-byte PNG, not a
bank receipt. This checks transport, kind selection, rights and exact consent;
it does not prove autonomous receipt interpretation or a native model provider.
Separate [native browser acceptance](shuttle-meal-final-browser-qa-2026-10-21.md)
and earlier [native proof/review QA](shuttle-meal-financial-runtime-qa-2026-10-21.md)
remain their own evidence. Exact native-preparation adoption remains open.

## Accepted behavior

An explicit mock-Telegram photo request reaches the reactive owner run. The
agent reads the meal order and submits its accepted opaque file ID as proof
for6.65BYN through zinusya_order_payments. The server retains order_kind meal.
One pending claim exists; there is no confirmed review or capacity allocation.

A separate fixture global administrator requests review. Loom reads the actual
evidence and fetches the Telegram-backed file through the review tool. The broker
artifact contains exactly68bytes and matches the original SHA256. The reviewer
saves an intention and prepares its canonical commit. Telegram delivery shows
the payer, original order, reported/frozen/confirmation amounts, proof timestamps,
event-local evidence deadline and approval expiry, reason, all four lunch
components and absence of limited resource requirements. The claim stays pending
until explicit consent; preparing a review does not confirm money.

Plain-text consent arrives as steering on the same administrator run. Its
interaction resolution binds the original approval ID and argument hash. The
original review completes once: evidence version2 is confirmed for665minor BYN,
with the expected actor, global_administrator authority and literal reason.
There is one immutable review, one meal order and zero capacity allocations.
Both Loom runs and all accepted tasks complete; the outbox is delivered.

The owner without review authority receives403, with event payment authority
required, from the actual evidence_for_review endpoint. Primary data and runtime
identities remain preserved throughout this separate stand's lifecycle.

## Review, retained failure and cleanup

The initial stand-creation helper retained an old evidence directory and refused
at its first guard, before creating containers. The original helper and failure
are retained. The corrected helper uses a new explicit directory and project;
no financial action is repeated to recover that preflight failure.

Independent textual review of the runtime probe and ownership-scoped retirement
reports no actionable findings. A separate database audit verifies exact money,
actor, reason and final counts. Both shuttle and Loom dumps are fully decoded;
all service logs are retained privately before removal. Label-based checks
confirm that no resources belonging to this disposable project remain.

Private evidence: .cache/shuttle-work/meal-financial-flow-v1/agent-payment-v1/
checkpoint.json, audit.json, review-denial-probe.json, review-v1.json,
retirement.json, acceptance.json and archive/. Secret tokens, mailbox contents,
database dumps and raw logs remain ignored. This is not production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
