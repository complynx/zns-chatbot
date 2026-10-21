# Mock Telegram connection and actor isolation

The simulator accepted another user's identity when clicking a private message's
keyboard. It also performed HTTPS handshakes in the listener's accept loop, so
one TCP client that sent no TLS ClientHello could stall unrelated HTTPS clients.

Private inline, reply and Web App keyboard actions now require the private chat's
recipient identity. Group actions still allow different users, and `/qa/update`
remains the explicit path for deliberate adversarial updates. This does not
change production Telegram or the bot's authorization rules.

TLS handshakes now run in daemon request threads after setting a 15-second socket
timeout. The accept loop continues serving other clients. This follows the
existing counted-provider fixture pattern.

## Verification

Both defects reproduced before the fix against actual localhost listeners:
another private recipient received 200 instead of 403 for all three keyboard
types; an idle TLS peer caused the next HTTPS health request to time out.
The fixed test checks rejection without webhook forwarding, legitimate recipient
actions and a successful HTTPS health request while the idle peer remains open.

The complete simulator suite passed all 29 tests in the pinned Python 3.14 Linux
image, without skips or an OOM kill. The existing group and process tests also
passed. The Windows host run passed with two intentional POSIX-process skips.
The Linux log retains ResourceWarnings about HTTPError fixture cleanup; these
were not test failures and are not claimed fixed by this change.

The Linux runner used a read-only frozen source copy, no external network,
512 MiB memory with no swap and a bounded temporary filesystem. The only sockets
used by the tests were synthetic localhost listeners. No actual Telegram, IAM,
Loom, developer stand or installed primary image was changed.

An independent source review reported no actionable findings. Local evidence:
`.cache/shuttle-work/mock-tg-isolation-v1/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
