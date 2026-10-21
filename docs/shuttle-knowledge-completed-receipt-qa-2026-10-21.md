# Completed knowledge receipts, 2026-10-21

The operation reader retains both the original preparation and successful
receipt in storage. Both API projections previously returned that preparation
for completed operations, including the full original Markdown. This contradicted
the documented bounded receipt contract and could load up to 98,304 source bytes
into the agent context during recovery.

Agent and Mini App responses now include preparation only for `prepared` and
`expired` actions. Completed actions retain their original bounded result; the
stored preparation and receipt remain unchanged.

The existing completed-response test now supplies both preparation and result,
matching the store's output. It fails for both transports on the original code.
With the fix, the complete API package passes locally. Additional response checks
preserve exact original content for prepared and expired actions on both
transports. These are serialized API contract checks, not database, real Loom or
Telegram QA. Publication and moderation behavior are unchanged by this projection
fix. Remote CI and primary installation of this change remain separate gates.

Retained evidence is in `.cache/shuttle-work/knowledge-completed-receipt-v1/`:
original failure and passing complete-package logs. No test containers were
needed or created; production was not changed.
