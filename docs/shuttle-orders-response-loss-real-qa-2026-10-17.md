# Orders: controlled lost response and clean bot restart

This is owned runtime acceptance on `shuttle-live-test`, not a production claim.
The actual Telegram user8968148043 used only private chat6087685431 with
`@complynx_test_bot`. The real embedded Mini App used Telegram initData and real
Zitadel authentication. No locally signed login fixture or model provider was
needed for this native scenario.

## Fault and observed recovery

The accepted image remained
`sha256:42a605470d00e189bc6a7096490b1e845ac340a3f229434b51f7a06db8ea1a40`,
with schema40. A dedicated loopback proxy on18383 forwarded to the same bot18382.
Only the owned Mini App origin and launch URL temporarily changed to18383.
The bot token, registration-detail encryption key, other environment, mounts,
ports, security settings and image were retained. Eight companions, including
Loom and IAM, retained their fingerprints. A private backup archive was checked
before changing the bot; existing business-table hashes were preserved.

In the existing synthetic native-only event, the old cancelled order and empty
draft2 remained. The actual form saved two soup portions as draft3 and reviewed
soup2 x12.34 BYN =24.68 BYN plus required service1.00 BYN, total25.68 BYN.
Line IDs and deadlines in Europe/Berlin were visible before explicit confirmation.

The proxy selected exactly that event's first submit, forwarded its original
operation/proposal, read and validated upstream200 with the submitted order, then
returned a controlled503 instead of the successful body. This models a successful
write whose result did not reach the caller. It is an injected HTTP failure,
not proof of physical network loss or a hard power failure. The proxy recorded
only operation/proposal/order IDs, status and hashes, not authorization/initData
or a raw response body.

The actual form displayed an unknown result, retained its recovery operation,
and disabled event, quantities, save/clear, review and confirm controls. It offered
`Check earlier order request` and `Retry same order request`. Database read-back
proved one new submitted order with the exact immutable miniapp-source receipt.

The same candidate bot container was cleanly stopped and started; stop exited0
without OOM, with the same image/key/configuration. The same poller was restored;
Loom and the other companions were untouched. The still-open Mini App session
then used `Check earlier order request`, without another submit. It displayed
order `621bf939-a2fa-4b83-8707-6ce002031a2e`, submitted/unpaid with total25.68 BYN.
The draft was empty at version4; recovery buttons disappeared. The old cancelled
order remained visible. The original receipt was unchanged, there was exactly
one new order and one draft clear, and task/Loom-run counts did not increase
between the committed-unknown observation and recovery.

The original18382 origin/launch configuration was then restored. Existing data
was checked again; the exact temporary proxy was removed and the same poller
resumed. The ten normal owned services remain running. No developer Loom stand,
server_configs, production, commit, push, PR or external issue was changed.

## QA helper verification and boundaries

A focused mock-upstream check proved ordinary forwarding, rejection of internal
paths, suppression of one validated upstream success, and forwarding of a
same-key retry without rearming. This check tests the proxy, not the product.
Independent textual review passes found and corrected missing partial-failure
cleanup, an unhashed proxy configuration, recovery when Compose has removed the
old bot, and weak same-poller checks on repeated cleanup. Fresh complete v4 review
found no actionable findings. Cleanup can restore partial/interrupted states
without marking acceptance true; this fallback was reviewed, not deliberately
faulted at every step on the live stand.

The first Playwright click to open the new command's form stopped on a fractional
coordinate error before an action. A fresh accessibility observation showed the
unchanged chat; the visible AX button opened the app. No command or submit was
resent. Later form behavior was observed directly; no exported screenshot file
is claimed.

Evidence: `.cache/shuttle-live-test/orders-response-loss-qa/checkpoint.json`,
`proxy-state/fault.json`, the private backup, and the source-bound proxy unit proof
under `.cache/shuttle-work/orders-response-proxy-unit/`. Helpers are
`orders_response_loss_real_qa.py` and `orders_response_fault_proxy.py` under
`.cache/shuttle-work/`. The proxy ran without writable root, with dropped
capabilities/no-new-privileges,128MiB memory/no swap,1CPU,32PIDs and8 request slots.
No product source or dependency changed for this acceptance.

A later live agent-first pending-proposal handoff and canonical replay passed;
see `shuttle-orders-shared-proposal-real-qa-2026-10-17.md`. It is sequential.
Native-first preparation and overlapping real HTTP/API canonical recovery later
passed; see `shuttle-orders-http-race-qa-2026-10-17.md` for its explicit fixture
authentication limits. Still required: actual same-key submit retry if a separate
runtime acceptance needs it, hard-kill/in-flight recovery boundaries, order proof
intake/capacity/payment review/fulfillment, and the remaining roadmap. This test
used receipt read-back, not the retry button. No unattended model interpretation
or production readiness is established.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
