# Accepted message recovery after its target run finishes

## Recovery contract

A saved control payload records intent. It does not prove that Loom accepted the
Input: the bot can stop between saving the payload and sending the request, or
lose the response after Loom commits it. Finishing the parent run does not resolve
this uncertainty.

When an ordinary accepted message has a frozen, uncertain Input and its target
has finished, the bot replays the exact payload to the same run with the accepted
task UUID as the idempotency key. It uses fresh user delegation under the same
issuer, subject, service actor and organization. This avoids relying on the
terminal parent's expiring auth session or changing its checkpoint.

A successful receipt proves acceptance, not successful execution. The control
finishes locally without an invented no-active reply or a replacement run.
Only HTTP 409 with structured error code `run_terminal` permits the existing
atomic promotion into a new run. The accepted task UUID and request are retained.
Generic conflicts, missing resources, service failures and lost responses preserve
the frozen intent in `awaiting_service`. A later unrelated rejection cannot
cancel an earlier uncertain effect. Current IAM denial/revocation gates still
apply before recovery.

This depends on Loom checking the retained idempotency record before terminal
state, and committing the input and its receipt together. Both properties were
verified in the owned `loom-upstream-1050` source copy. Its idempotency scope does
not include the auth session ID. Receipt retention must cover the entire unresolved
retry lifetime; destructive restore or receipt purging invalidates the absence
proof. Keep the service actor ID stable while work is unresolved; credential
rotation for the same identity is supported, but replacing the service identity
requires a drained queue or a separate reviewed recovery migration. No Loom checkout or stand was changed.
