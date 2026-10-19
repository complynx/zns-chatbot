# Owned test poller shutdown QA — 2026-10-16

The first main-stand prelink cutover stopped at ingress shutdown, before stopping
or replacing the bot and before migration. The poller received SIGKILL after its
five-second Docker stop allowance: exit137, OOM=false. This failed attempt remains
in ignored `.cache/shuttle-live-test/prelink-main-cutover/`; it is not a passed
cutover. The running bot still used the old image and schema32 at that boundary.

The poller installed a handler only after Telegram preflight, then merely set a
flag. A blocked preflight request had no application handler; a blocked long poll
could continue for25 seconds despite a stop request. The handler now installs
before the first HTTP request and raises SystemExit(0), interrupting HTTP waits
and bypassing retry handlers. The cursor still changes only after ingress accepts
an allowed update, through atomic file replacement. Interruption before replacement
may replay an already accepted update; the bot must retain update-ID deduplication.
No transport-level exactly-once delivery is claimed.

A Linux POSIX subprocess test first failed against the old source: preflight
exited on signal and both blocked polling/ingress cases exceeded one second.
After the fix, all three blocked HTTP stages exited0 within the test's one-second
wait. The pending cursor stayed unchanged. Restart after an unaccepted ingress
request redelivered that update and advanced the cursor only after acceptance.
This fixture does not cover the separate uncertain case where ingress committed
but its response was interrupted; existing bot deduplication remains necessary.

The complete stand ingress/egress guard suite passed11 tests in Linux, with no
skips, using HTTP fixtures and real cursor files. It made no Telegram requests.
The two temporary QA containers used the pinned Python image,128MiB/no swap,
1CPU/64PID, read-only filesystem and no external network. Both finished0 without
OOM and were removed by their exact verified identities. Logs and terminal states
remain in ignored `.cache/shuttle-work/poller-shutdown-qa/`.

The original main poller container was then started with the fixed bind-mounted
source. Its expected bot/account allowlist was checked, and a fresh successful
polling cycle was observed without container replacement or restart-count change.
An independent textual source/test review found no actionable issue. The index
remained empty; no commit, push or production deployment occurred.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
