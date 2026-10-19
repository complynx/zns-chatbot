# Mock provider lifecycle QA — 2026-10-18

The isolated Telegram QA factory now gives the counted provider its own network
namespace. Loom uses `https://provider.zinusya.localhost:18089/v1`, with a
project-private DNS alias and the stand's own CA. No provider port is published.
Restarting Loom no longer requires a provider namespace restart. The shared
counted-provider source, shared CA issuer and primary stand are unchanged.

## Implementation and checks

The mock-only entrypoint reuses the shared server and handler. TLS handshakes run
in daemon request threads after the accepted socket receives its five-second
I/O timeout. A client that sends no ClientHello does not block other requests.
The provider runs as UID 1000, matching the private TLS key; fresh provisioning
sets its mailbox owner to UID 1000 and mode 0700. SIGTERM exits the process without
waiting for pending inference handlers. Already published request evidence stays
in the mailbox; no response is invented. This is not a request-drain guarantee.

New manifests carry `provider_layout: project-network-tls-v1`. Legacy `start`
refuses before Docker operations. Preserve old evidence and create a fresh named
stand; legacy `status`, `stop` and `destroy` remain available.

All 31 Linux behavioral tests passed in a networkless, read-only, bounded job,
including verified TLS, a stalled TLS peer, SIGTERM with an unanswered inference,
unchanged request bytes, absent response evidence and legacy-start refusal.
The runner used 256MiB RAM/swap, one CPU, 64 PIDs and 128MiB temporary storage.
Two fresh final reviews found no actionable correctness or safety issues.

## Real IAM/Loom acceptance

Fresh `provider-final` used pinned local `mvp-56`/schema49 and frozen Loom fc6c574,
its own PostgreSQL, Zitadel, CA, fake Telegram credentials and provider mailbox.

1. Fake Telegram update 7301 created a real user/task/session/run. Loom reached the
   TLS provider; one manually supplied final inference completed one task/outbox/send.
2. Only the same Loom container stopped and restarted. Its stop command took
   0.593s and exited 0. Provider ID and StartedAt stayed unchanged, as did all
   eight companion process fingerprints. Loom became ready again.
3. Update 7302 reached the same provider through real Loom. Its exact manual final
   response completed a second task/outbox/send. Final counts were two completed
   tasks, two completed runs, two delivered outbox rows, two send attempts and
   two visible bot messages. No Loom write operation occurred.
4. The provider then stopped in 0.437s, exit 0, without OOM or Docker kill. These
   durations measure the Docker stop command, not internal application drain.

The Linux process test separately stopped the provider while an inference was
unanswered. The live Compose shutdown above occurred after both tasks completed.

## Failures retained

The first combined test runner had shallow source mounts and 16MiB temporary
storage; imports and a large-state SQLite test failed. Its corrected runner passed.
The first network layout used private HTTP; Loom correctly rejected HTTP outside
loopback. The fixture was retired without accepted messages. Review then found a
missing DNS alias and Linux key-owner mismatch; both were fixed. TLS process
checks also failed before explicit worker handshake and certificate-SAN alignment
were complete. The final process test verifies `127.0.0.1`, matching its public
fixture certificate; live Compose uses the CA-covered DNS alias.

The final task check initially compared newest-first message snapshots as though
they were chronological. Inspecting the same state showed two successful replies;
comparison by message ID passed. No update or inference response was repeated.
Private logs, failed attempts and corrected checks remain in ignored evidence.

## Preservation and cleanup

Application and Loom archives were retained before retirement: 299249 and 94488
bytes, SHA-256 `0021d5977fa19de99e721b973ceaffc9175cbc13696506727d65f919a4f19335`
and `bca1c757cabdf96951cf1d5751836e714fe71de4f1586201658e1498a3af5812`.
The primary stand's bot/poller epochs and 132 Loom runs were unchanged. All 112
fingerprints matched except River leader/queue bookkeeping; all 46 River job rows
were identical. No business drift or retention exemption was needed.

All three temporary Compose projects and all test runners were removed. The final
project has zero containers, volumes or networks. Credentials, mailboxes, database
archives, runtime receipts and checkpoint remain ignored under
`.cache/shuttle-work/mock-provider-lifecycle-v1-qa` and named mock state folders.

This proves fixture lifecycle and specified real IAM/Loom routing with synthetic
Telegram and manually supplied inference. It does not prove real Telegram
transport, unattended model behavior, continuous-bot IAM-first outage recovery,
existing delegated-session expiry or abrupt Loom kill. The subsequent [IAM-first continuous-bot gate](shuttle-iam-first-continuous-qa-2026-10-18.md)
now passes that restoration path; the other listed gates remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
