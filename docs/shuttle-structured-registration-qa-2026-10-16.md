# Structured registration owned QA, 2026-10-16

Owned shuttle-live-test image
`sha256:fffc16debed61115a3428a307c31e4f87e30d015233468bb2e6206983f2a0bf8`
runs on schema34. This report accepts solo waiting-list entry only. It does not
accept full registration/pass parity, production deployment or unattended model
behavior. All Telegram interaction used the authorized current account in the
private @complynx_test_bot chat. No production or Loom developer stand changed.

## Build and rollout

The complete unchanged Dockerfile built684 frozen inputs into a57,630,651-byte
image. Source hashes remained unchanged. The dedicated builder had2GiB memory,
no extra swap,2 CPUs and one worker. memory.events recorded max1650 limit-hit
events and zero oom/oom_kill/oom_group_kill events. max is a count, not peak memory.
The builder was intentionally stopped (exit1/noOOM) and removed with its own cache.
The candidate and previous images remain available.

The image-only cutover stopped the existing poller and bot, required drained
intake/outbox and saved a2,261,940-byte pg_dump archive with its hash. All65 table
hashes and schema34 were preserved across replacement. Eight companion services
kept their identities/configuration. Real-IAM owner profile read changed no table;
the same poller resumed with a fresh successful cycle. Private prelink stays
disabled. A binary rollback requires ingress stop plus reconciliation/drain of
new registration callbacks/checkpoints; retain database records and receipts.

## Native acceptance

Four fresh synthetic events were created through the owned real-IAM admin API.
They use Europe/Minsk, registration availability and artificial pricing. They are
fixtures, not real sales. The profile stayed at version18, with its previous name
and language intact. Exactly12 tasks completed and their outbox replies delivered.

- Buttons only: /register, next page and readable event A selection worked while
  Loom was stopped. The same bot container stopped cleanly (exit0/noOOM), restarted,
  and accepted the previously delivered Leader button. Canonical operation status
  recognized the receipt. A second Leader click returned the existing registration;
  Follower returned refusal. Delivered response text and one unchanged row were
  checked, not just successful task states. No Loom run/input was created.
- Buttons to agent: event B was selected through the native menu. The next natural
  user request contained no event ID. The actual Loom model request contained the
  bounded selected_event history; authenticated context resolution and join
  created B's follower waiting-list entry and delivered the reply.
- Agent to buttons: the request for C explicitly allowed reads only. The real-Loom
  context tool read did not create a registration. /register plus C's ID then
  displayed its role choice; Follower created its waiting-list entry without an
  additional Loom run. This proves read/selection-to-write continuation, not a
  pre-existing agent-created registration being edited through the menu.
- Agent only: a natural request identified D and Leader. Real-Loom authenticated
  context and join tools created the waiting-list entry and delivered the reply.

There are exactly four fixture registrations, all owned by the current account:
A leader, B follower, C follower and D leader. No pass, pair or payment was created.
Thirteen protected table hashes stayed unchanged, including all events, separately
stored registration/deposit policies, all other registrations, passes, pairs,
payment evidence/reviews, profile, settings and persistent event context. Loom
counts changed from71 runs/25 inputs to74/25: exactly three distinct agent tasks.
The same owned Loom container was restored after its intentional clean stop;
all companion identities and the poller were preserved. The bot stayed on the
same candidate container after its restart. No temporary QA container remains.

## Review and evidence boundaries

The final component source had92 root tests /174 pass events under scoped
Linux/race checks, no failures/skips and zero strict-lint issues. No product source
changed during build/native QA. These checks are scoped, not a full-suite rerun.
Architecture/Go-senior discussion, source review and cutover review completed.
Native harness reviews found and fixed protection for foreign fixture-event
registrations, user-visible duplicate/refusal assertions and separate policy-table
hashes. A fresh review of the final harness found no actionable findings. The index
remained unchanged; no commit, push or publication was made.

The real Loom provider was manually driven through its existing provider protocol.
One summary step was handled as text only. This tests native Telegram ingress,
actual IAM authentication, tool contracts, durable tasks and delivered output. It
does not test independent event/role decisions by a language model. The final
Telegram reply was checked visually in the browser. No new screenshot file was
saved; earlier screenshot-write failures remain documented and historical images
are not evidence for this slice.

Ignored evidence: .cache/shuttle-work/structured-register-final-qa/,
.cache/shuttle-work/structured-register-image-qa/,
.cache/shuttle-live-test/structured-register-main-cutover/ and
.cache/shuttle-live-test/structured-register-native-qa/. Stable fixture IDs,
operation/task IDs and exact delivered replies are retained in their checkpoints;
credentials are not included in this report. Synthetic fixtures remain as history.

Full administrator allocation, pass/pair/payment forms, configurable name/passport
prerequisites, trusted Mini App/WebView login, real zitadeltg browser linking and
agent question/approval reply keyboards remain open. The next steps retain all
four required interface paths; catalog/status reads alone do not close them.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
