# Native pass deadline and late decisions, 2026-10-20

A separate owned mock-Telegram/real-Zitadel/Edge stand passes the native initial-
deadline and late-human-decision gate on schema64 / `zinusya/mvp-77`. The installed
eleven-service primary and its134 Loom runs remain unchanged. No production or
Loom developer stand is changed.

## Actual workflow and results

Four synthetic IAM identities represent an administrator, an ambassador and two
participants. Both participants supply required fields, explicitly register and
consent to their pair through real commands/forms. Before any assignment, fixture
configuration sets a45-second initial payment window, a100.00-BYN paired tariff
and a30.00-BYN minimum deposit with a25-percent minimum share. Event sales closing
is15 minutes after that configuration. No pass, evidence or review is seeded and
no assigned deadline is rewritten.

The administrator prepares and explicitly confirms the two passes in the actual
form. Each deadline is checked against assignment time and the configured closing
cap. The leader sends a file before the assigned deadline. The bot restarts
normally before that deadline; both exact unpaid passes and pair/registration
membership remain unchanged after the deadline.

The leader then submits that original file after the deadline. It remains timely
because its stored Telegram send timestamp precedes the frozen pass deadline.
Submission is pending and credits zero. A later explicit ambassador decision
credits30.00 BYN.

The follower sends a new file after the deadline. Preparing confirmation without
a reason returns400 and changes no money. The ambassador explicitly rejects that
proof with a reason, crediting zero. A new corrected late file is submitted and
explicitly accepted with an overdue-payment reason. Final state:

- Two original assigned passes remain, each priced100.00 BYN.
- Three immutable original submission receipts remain pending as historical results.
- Two confirmed reviews credit60.00 BYN total; one rejection credits zero.
- Each pass satisfies its30.00-BYN deposit, remains short of full payment and owes70.00 BYN.

Every stored financial row is bound to the intended owner, pass, original file,
amount, currency, deadline, timestamps, reviewer and decision reason. The evidence
deadline is compared with the original pass, rather than with another copy of
the evidence itself. No Loom run is created: Loom is stopped throughout the native
workflow. Three media task records remain in the archived test database; draining
those queued agent tasks is not part of this gate.

## Retained failure and recovery

The first rejection browser fails before any POST: it tries to fill the amount
that the UI intentionally disables for rejection. Its terminal result, closed
browser, zero-write request audit and original checkpoint are retained. Database
read-back confirms the follower still has pending evidence, zero credit and no
review workspace.

A separately reviewed driver correction checks the disabled field instead of
filling it. Recovery requires that exact terminal read-only failure and matching
stored state, then continues with a fresh reservation. Earlier submissions and
reviews are not repeated. There are14 actual browser invocations:13 successful
and one retained read-only failure. All contexts and browsers close. No application
source change is needed.

Manual screenshot inspection confirms the45-second window, both identities,
Minsk-local deadlines, a saved rejection for0.00 BYN and the corrected late
confirmation for30.00 BYN with its reason and70.00-BYN balance.

The restart's Docker stop command takes1.484 seconds, exits0 and has no OOM.
Container start timestamps independently place the restart between assignment
and the actual deadline. A later bounded historical-event query returns no
original signal/exit pair; that unavailable measurement is retained. This gate
does not establish full-process exit within one second, and no extra restart is
performed merely to obtain a timing number.

## Reviews, cleanup and limits

Fresh normal and adversarial preflight reviews finish clean after two rounds.
They strengthen deadline and persisted allocation bindings. The bounded driver
correction/recovery also receives two fresh clean independent reviews.

All nine owned services stop with exit0 and no OOM. After stopping writers, the
Shuttle, Loom and Zitadel archives are fully decoded; the exact owned containers,
volumes and network are removed. Decoding is not fresh restore acceptance.
Ignored evidence is in `.cache/shuttle-work/pass-deadline-runtime-v1-qa`, with
review/source freezes in the adjacent `pass-deadline-*` records. The Git index
remains empty.

This accepts the native initial-deadline/late-decision slice. It does not cross
sales closing or accept top-up closing behavior, agent-driven deadline variants,
real Telegram WebView, unattended model comprehension, Linux/race or production.
Keep those remaining requirements in the broader hardening work. Next extend
whole orders/meals and massage acceptance; earlier [whole happy paths](shuttle-registration-pass-parity-plan.md)
retain their separately documented versions and controlled-provider boundaries.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
