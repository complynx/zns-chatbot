# Joined mixed registration, pair and payment QA — 2026-10-20

Both whole mixed happy paths pass on separate owned stands using real local
Zitadel and Loom, mock Telegram and actual headless Edge forms. These checks use
schema64 / `zinusya/mvp-75`. The owned primary remains schema64 / `mvp-74`, with
its eleven services and 134 original Loom runs preserved. This report does not
install the candidate or accept production deployment.

## Two fresh journeys

Each stand starts with four synthetic IAM users: two participants, one event
administrator and one ambassador. Fixtures configure one Minsk event, authorities
and payment rules only. No participant profile, legal details, registration,
pair, pass or payment is seeded. The pass costs 100.00 BYN; the configured minimum
deposit is 30.00 BYN and minimum share is 25 percent.

| Journey | Registration and pair consent | Assignment | Deposit | Top-up | Completed Loom runs |
| --- | --- | --- | --- | --- | --- |
| Native to agent | Actual commands, callbacks and forms with Loom stopped | Native preparation, agent confirmation | Native submission and review | Agent submission and review | 9 |
| Agent to native | Actual Loom tools and separate participant messages | Agent preparation, native confirmation | Agent submission and review | Native submission and review with Loom stopped | 13 |

Both journeys retain the original registration IDs, pair membership and pass IDs
through the handoffs. Each ends with two assigned passes, four evidence records,
four ambassador reviews and 20,000 confirmed minor units. Together they verify
four paid passes and 40,000 minor units, with all tasks and outbox work completed.
These are distinct cohorts, not replays of the same business writes.

Native evidence remains pending until ambassador confirmation. Each later agent
reads the original native submission operation through the discoverable
`zinusya_payments.operation` tool. Four full original pending receipts are compared
with their retained originals, separately from current confirmed balances. Eight
original attachment captions are checked against actual Loom inputs. When Loom
returns after native payment actions, queued files are acknowledged without
changing the financial state. Final agent reads and account forms agree on the
same registrations, pair members, pass owners and fully paid balances.

## Restart and response recovery

The original assignment proposal, approval, task and Loom run survive graceful
bot restart in both stands. Live matched Docker SIGTERM and clean exit events
measure 0.456657819 seconds for native-to-agent and 0.434941719 seconds for
agent-to-native. Docker stop command wall times are 0.641 and 0.656 seconds.
Neither probe requires SIGKILL or records an OOM. These are individual probes,
not a bound established for all workloads.

The agent-to-native assignment deliberately loses the successful native HTTP
response. A fresh browser context recovers the original complete receipt through
a read. Exactly one confirmation POST creates the two passes. The agent then
reconciles that same result instead of assigning again.

## Source change and verification

The mixed gate exposed a missing read tool: agent context carried a native
operation ID, but the normal discoverable API could not retrieve its original
receipt. Candidate mvp75 adds an actor-scoped receipt read through the existing
store and current evidence access checks. Unknown or another actor's operation
returns a null receipt; that is not proof that an operation never ran. It grants
no new write permission and does not renew a proposal.

Two new PostgreSQL behavioral roots fail with HTTP404 before the change. After
it, 38 affected PostgreSQL roots plus a separate native-context root pass, as do
affected API/task units, lint and Linux cross-build. An earlier incorrectly
anchored test selector ran one existing test only; it is not counted as proof of
the new cases. Lint formatting findings were corrected before the passing run.
Fresh ordinary and code-quality reviews of the API and each mixed controller
finish without actionable findings. The separate mock-Telegram SIGTERM fix is
also exercised by clean stand shutdown.

Candidate identity:

- Image: `sha256:6b38495e31d985db6f16970646a52f2227d3fca89ad194e4f5e625788ca459f5`.
- Binary SHA256: `29b4d1fc447f2080d2e23a949b033569a19e0f02c1e1721880743c3eb4f883cb`.
- Product source SHA256: `90cd647f58b1e9e5f316cc63fe666a2b78c635d5a5b2a17583f835e77e79707a`.

## Retained evidence and limits

Local ignored evidence is in `.cache/shuttle-work/joined-mixed-na-runtime-v1-qa`
and `.cache/shuttle-work/joined-mixed-an-runtime-v1-qa`: runtime acceptance,
caption audits, original receipts, restart events, screenshots and cleanup proof.
Component evidence is in `.cache/shuttle-work/payment-operation-read-v1`.
Both nine-service temporary stands are removed after stopping writers and fully
decoding three database archives each. All eighteen services exit with code0 and
no OOM. Decodable archives are not a fresh restore acceptance test.

Manual screenshot inspection confirms participant account balances and native
review/assignment details. It also identifies an unresolved native assignment
presentation defect: the form shows the effective deposit, but omits the
configured minimum percentage and prints internal IDs/raw seconds from the
canonical description. This report accepts workflow continuity and financial
results, not complete presentation of every policy term. Fix the view without
rewriting the stored proposal or receipt.

Controlled provider scripts prove real orchestration and tool contracts, not
unattended model understanding or document interpretation. These runs do not
accept real Telegram WebView, a new full Go/race suite, remote CI, broad deadline
and failure combinations, archive restoration or production readiness. The prior
[native](shuttle-joined-native-runtime-qa-2026-10-20.md) and
[agent](shuttle-joined-agent-runtime-qa-2026-10-20.md) whole happy paths used mvp74;
this is not a rerun of all four paths on one release.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
