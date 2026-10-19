# Two-customer order capacity: isolated browser QA

The current schema49 / `mvp-56` implementation passes a two-customer last-capacity
scenario through the shipped Mini App forms. Two actual native submissions overlapped:
one held the two available units and the other became exhausted with no allocation.
An explicit ambassador rejection then released the hold without crediting money or
promoting the exhausted claim.

This used real PostgreSQL, Zitadel, Loom and application HTML/JS/API on the owned
`zinusya-mock-orders-capacity` project, ports21530..21535. Telegram and its SDK were
synthetic. File intake passed through real Loom with manually supplied no-tool
responses. This proves browser/backend behavior, not unattended model interpretation,
real Telegram WebView behavior, certificate trust or production acceptance.

## Fixtures and observed behavior

Four synthetic users authenticated through normal Mini App bootstrap: administrator,
two customers and a separate event ambassador. Only the fixture administrator had
global IAM authority. Each customer had an assigned unpaid pass. The ambassador had
no participant pass and only event-scoped review authority.

Normal APIs created two distinct orders, each with two soups at12.34 BYN plus a
1.00 BYN service charge, total25.68 BYN. The shared resource ceiling was two units.
Order submission alone held no capacity. Each customer submitted a separate valid
synthetic PDF through mock Telegram and normal file intake. Both original file tasks
and Loom runs completed; neither file response made business tool calls.

Separate isolated browser contexts opened the actual forms with fresh signed mock
Telegram SDK data. Each listed only its own file, and protected preview bytes matched
that customer's source PDF hash. Foreign file access was denied. Each customer
submitted a full-price claim for their own immutable order and file.

A brief owned PostgreSQL event-row lock delayed admission. Before release, the
observer recorded both native POSTs and two active database lock waiters in the
blocking chain. Both submissions were observed within0.25 seconds after the start
signal. The barrier was released within the native five-second request budget.
Thus this was overlapping admission, not two sequential requests labelled a race.

One claim became pending and held exactly two units. The other became exhausted
with no hold. No financial review or credit existed at that point. Both customers
and the ambassador received the correct pending/exhausted notices. The exhausted
customer was told that places had run out; the ambassador was told that a refund
required attention. No refund was asserted or performed.

The ambassador then opened the admitted claim's actual native review form, inspected
the protected proof, prepared rejection and confirmed it once. Final persistence
checks found exactly one review: rejected, zero credited, attributed to that
ambassador and its IAM subject. The exact held allocation became released. Both
orders remained unchanged; the exhausted claim's complete row remained unchanged.
No automatic promotion occurred after capacity became free.

The two native submissions and native review created no additional agent task,
Loom run or Loom write operation. The original two file tasks and runs remained
complete. All six domain notices were delivered with receipts addressed to the
correct synthetic customer or ambassador chat. Native operation IDs and stored
results matched the browser journal.

## Review, evidence and cleanup

Independent correctness and safety reviews found no actionable findings before
business execution. Their review was static; the source-gate manifest was generated
and checked by the root agent afterwards. All nine service ownership labels,
container IDs, images and start epochs matched before execution and archival.
The browser journal uses exclusive creation and records intent before publishing
requests; this scenario ran once. Browser contexts were closed and the owned lock
was released.

Private evidence is retained under
`.cache/shuttle-work/orders-capacity-browser-v1-qa`: checkpoint, browser journal,
screenshots, source hashes, runtime manifest, database dumps, logs and primary
preservation checks. The application archive is316545 bytes,
SHA-256 `398f31267761b8470a41cf928b94e7db29f9b745a255b292de057cdf36779bcc`.
The Loom archive is96895 bytes,
SHA-256 `7c5e6927ca348ea9bc4de6b6939f8db3316136df08f996c5e3dbfee75d94989e`.
Both custom archive headers and tables of contents were checked. Restore was not
exercised in this gate.

The disposable project's containers, volumes and network were removed. The primary
stand's installed runtime/configuration, business fingerprints, original46 River
jobs and canonical queue state passed the existing preservation checks. No product
Go code, primary deployment, real Telegram conversation or developer Loom stand
was changed. No staging, commit or publication occurred.

This gate does not decide mixed-resource allocation, partial/cash order payments,
late proof binding or re-admission policy. It does not prove a refund, stress-test
throughput or recovery from SIGKILL. Normal restart remains SIGTERM plus graceful
shutdown; exceptional ambiguous external outcomes may require human/agent
reconciliation from durable identifiers and logs.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
