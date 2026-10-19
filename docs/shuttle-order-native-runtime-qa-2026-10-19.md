# Native order review and agent continuation: owned runtime QA

The schema52 / `mvp-59` candidate passes native ambassador confirmation with its
Loom stopped, and agent → form → original-agent-run continuation on a separate
owned mock-Telegram stand. No application code changed for this gate.
Candidate image:
`sha256:532bdc8f3881b4572bbca9a52dc1880cc69c5d61a4208e1951009cf0cb16b8ff`.

The shipped HTML/JS and APIs, PostgreSQL, Zitadel and Loom were real. Telegram
and its SDK were synthetic; manual responses drove the counted model provider.
This proves the scoped browser/business contracts. Real Telegram WebView,
unattended model interpretation and production acceptance remain separate.
The [earlier real input barrier](shuttle-order-reviewer-dialogue-qa-2026-10-18.md)
and [schema49 handoff proof](shuttle-order-browser-handoff-qa-2026-10-18.md)
remain unchanged historical records.

## Fixtures and observed behavior

The isolated nine-service project `zinusya-mock-order-review-native-r1` used
loopback ports21700..21705, separate databases, CA, credentials and provider mailbox.
Three fresh synthetic identities signed in through normal Mini App bootstrap.
Only the administrator received global IAM authority. The owner held an assigned,
unpaid pass in each event; the separate reviewer had event-scoped ambassador
rights. Each independent event used Europe/Berlin and capacity for two soup units.

Each order totaled2×12.34 BYN plus1.00 BYN required service charge, or25.68 BYN.
Two distinct valid PDF files were accepted through normal bot intake. These two
file turns used actual Loom but called no business tools. Fixture setup then
bound each accepted file to its explicit order/version. Both claims were pending,
with two held units each and no confirmed money. This is fixture preparation,
not acceptance of the complete owner registration or order-entry UI.

1. For the native-only case, Loom stopped cleanly with exit0 and no OOM. The
   reviewer opened the shipped form, prepared one decision and inspected its PDF.
   Confirmation remained disabled before proof loading. The loaded633-byte blob
   matched the original SHA-256. One explicit confirmation completed the original
   workspace/proposal; no replacement save was sent. One ambassador decision
   confirmed2568 minor units and changed the same allocation from held to occupied.
   The task/run counts stayed at the two file-fixture turns. The same Loom
   container was then started again.
2. For the mixed case, an explicit reviewer message started one actual Loom run.
   Its broker saved revision1 and called review_commit for the unchanged full
   decision. The original task waited for consent while money stayed pending.
   The shipped form loaded that workspace, inspected the original PDF and
   confirmed the same proposal without saving another revision. The task remained
   awaiting the original approval. A new explicit free-text message continued that
   conversation. interaction_resolve returned exactly the native canonical receipt;
   the same run completed without another financial decision.
3. Final database read-back found two submitted orders, two confirmed claims,
   two ambassador reviews, two completed workspaces and two occupied allocations
   of two units each. Four tasks and three Loom runs were completed: two file
   turns, one reviewer run and its steering task. Confirmation did not fulfill
   either order or pay either participant pass.

## Retained failures and review

The fixture observer first queried update_id on tasks instead of joining inbox.
It stopped after the first message was accepted. Read-back identified that exact
original task/run/file, and preparation continued without resending it. The failed
checkpoint and SQL error remain retained.

The first browser observer stopped after saving the native workspace: Playwright
reported an empty response body for the attachment. An independent authorized
HTTP read returned633 correct bytes. A read-only browser probe using reused launch
input timed out before the form loaded. A fresh signed probe loaded the page and
proved its actual633-byte blob had the source hash, even while Playwright still
reported the empty network body. The gate now hashes the blob exposed by the
shipped download link. It continued the same unexpired saved workspace and
confirmed once. These failures were observer failures, not evidence of a lost PDF
or a financial decision. Original files/logs were retained without rewriting PASS.

Independent reviews corrected the preview-render wait and the mixed observer's
result lookup: interaction_resolve carries the resumed result under its own call
ID. The final receipt screenshot waits for the post-commit detail refresh. The
native-only screenshot was taken earlier, with the accepted receipt visible while
its detail refresh was still pending; final database assertions establish its
completed state. No application behavior was changed to accommodate these checks.

## Cleanup and boundaries

Both PostgreSQL custom archives passed pg_restore --list:323458 bytes for the bot
and118347 bytes for Loom. All nine service logs were retained. The exact owned
containers, volumes and network were removed. The original eleven-service primary
passed its existing image/config/runtime guard and domain-preservation check,
remaining schema49 / `mvp-56`. The candidate stays uninstalled. No production,
Loom developer stand, real Telegram chat or publication was changed.

Ignored evidence: `.cache/shuttle-work/order-native-runtime-v1-qa/`, including the
original observer failures, browser results, final read-back, archives and cleanup
proof. This gate does not settle D23 partial/cash/refund policy or the jointly
deferred CA/zitadeltg/real WebView session.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
