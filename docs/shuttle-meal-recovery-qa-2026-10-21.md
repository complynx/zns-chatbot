# Meal review recovery during IAM and Loom downtime, 2026-10-21

Status: the bounded runtime scenario passes on the owned `meal-recover-r1`
fixture. Both databases are archived and fully decoded; all nine services,
their project network and volumes are removed. This report does not replace
full CI or the deferred joint Telegram/ZitadelTG acceptance.

## Fixture and consent

The isolated nine-service stand uses actual Zitadel, Loom and PostgreSQL, mock
Telegram and a controlled synthetic mailbox provider. The bot image is
`sha256:85acc5a4c5e488947ce1e633eb6c301c88545be26a019efdf6ef036ffc608d69`,
the same schema69/mvp-89 release installed on our primary. Application source
does not change for this check.

Direct API setup creates one synthetic meal order for 6.65 BYN. The owner then
submits a Telegram-backed proof through actual Loom. A separate synthetic
administrator reads the proof and prepares a review through Loom. The delivered
approval contains the payer, frozen meal composition, amount, event-local
deadline, reason and approval expiry. No money is confirmed before consent.

The proof submission and review use two distinct Loom runs. The counted provider
selects the tools and supplies the later explicit approval response; this is
controlled integration evidence, not autonomous interpretation of a bank receipt
or a real person's consent.

## Observed outage and recovery

After the approval delivery is durably acknowledged, only this fixture's Zitadel
and Loom containers are stopped. Both exit normally with code 0. A plain-text
consent message is accepted through mock Telegram while both remain stopped.
Its task and inbox record exist before the bot is stopped.

The bot receives its normal stop signal through `docker stop --time 5`, exits
with code 0 and no OOM, and starts again while IAM and Loom are still down. The
stop takes 1.703 seconds including Docker CLI overhead. Three observations,
separated by one second, find the restarted process running and both dependencies
stopped. The accepted response remains unfinished. There is still one order,
one pending proof, no financial review and no capacity allocation.

Zitadel and Loom are then started, without replacing any container or database.
The original review task and run resume. The next provider request carries the
accepted response as steering input and the original approval ID and argument
hash. Resolving that approval produces one confirmed proof at version 2 and one
review for 665 minor units, attributed to the original synthetic administrator
with `global_administrator` authority and the original reason.

Final readback finds one order, one evidence record, one review and no capacity
allocation. Both original Loom runs, all bot tasks and all outbox deliveries are
complete. An ordinary owner receives HTTP403 from the review-only evidence API.
No message, consent or financial write is resent to recover the result.

## Retained QA failure

After the financial commit, the first result-observation query incorrectly named
the review actor column `actor` instead of `actor_id`. It failed without changing
financial state. The original controller, SQL error and checkpoint are preserved.
A separate resume entry point reads the existing confirmed proof and review,
then supplies only the pending model's completion reply. It does not repeat the
consent or `interaction_resolve` call.

## Scope and evidence

This accepts a short simultaneous dependency outage and graceful application
restart during a pending meal review. It does not establish SIGKILL, host or
storage failure recovery, prolonged outage behavior, consent after expiry,
permission revocation, or autonomous model quality. No production service or
Loom developer stand is changed.

Private evidence is under
`.cache/shuttle-work/meal-financial-flow-v1/recovery-v1/`: `checkpoint.json`
retains the original task/run/approval and accepted input, `audit.json` records
the exact review actor and financial counts, and the observation-failure copies
retain the original unsuccessful QA query. `retirement.json` records both dump
hashes, full decoding, project resource removal and the final primary readback.
Mock Telegram's original file bytes/history and the provider mailbox remain in
their host bind directories; Loom's derived artifact volume is removed.

The source controller and retirement procedure pass independent textual reviews.
The primary's domain and runtime readback remain unchanged. No extra recovery
stand is left running.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
