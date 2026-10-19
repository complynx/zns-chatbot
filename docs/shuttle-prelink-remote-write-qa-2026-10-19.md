# Prelink accepted-write recovery QA, 2026-10-19

## Scope

The existing trusted prelink backend passed accepted-IAM-write recovery using the
real Linux bot image installed on the owned primary:
`sha256:68d82bd9d01e15a6c04464e36555c4e9252acd4a273f93b2f96798a2a74292f4`
(schema59/mvp68). No product code change was needed for these scenarios.

The separate prelink-write-r1 project ran four services: fresh PostgreSQL, fresh
Zitadel, that bot image, and a small TLS fault peer. It used its own credentials,
CA, organization and non-login-enabled JWT IdP. Provider assertions were signed
synthetic proofs; the actual zitadeltg executable and Telegram Web Login were not
part of this gate. No Telegram message, Loom task or browser interaction occurred.
The primary's eleven services, runtime identity and domain state were preserved.
No production or Loom developer service was accessed or changed.

The peer forwarded IAM requests over verified TLS. Only the fixture bot's host
mapping directed its configured IAM issuer through the peer; the issuer and IAM
signatures stayed unchanged. JWKS was also served through verified HTTPS. The
private backend API request used the stand's internal Docker hop with its separate
service bearer; this does not reaccept the external TLS ingress or browser flow.
The fault peer mounted no CA private key or assertion signing private key.

## Three accepted paths

1. **Immediate read-back.** IAM accepted account creation and link creation; the
   peer discarded each successful response. Both read-backs remained available.
   The original prelink request returned200 and completed one canonical receipt.
2. **Creation accepted, read-back unavailable.** The peer discarded the accepted
   account-creation response and temporarily denied account read-back. Prelink
   returned503. The local receipt stayed pending with an unset subject; an
   independent authenticated IAM read found the account at the already persisted
   local UUID. The actual bot stopped normally in0.422s, exit0/no OOM, and started
   again in the same container. After releasing the fault, the identical fresh
   signed assertion completed that same receipt and UUID.
3. **Link accepted, read-back unavailable.** IAM accepted the account and link,
   but the link reply was discarded and link read-back denied. Prelink returned503
   and retained a pending unbound local receipt. The same bot container crossed a
   normal0.484s shutdown, exit0/no OOM, and restart. Releasing the fault and sending
   the identical still-fresh assertion completed the original receipt and binding.

The times measure shutdown only. Recovery checks required the original JTI,
assertion and unexpired proof; no expired assertion was replayed or replaced to
make a case pass. These are actual process restarts, not reconstructed clients.

## Authoritative reconciliation

Each case retained one local user whose UUID equalled its exact IAM subject,
one receipt and one provider link with a deliberately nonnumeric opaque subject.
Direct authenticated IAM reads checked organization, synthetic email and the
exact IdP/subject tuple. The peer observed one create request and one link request
accepted by IAM per case, with exactly one wire attempt for each. Final total:
three users, three completed prelink receipts and three links. Four successful
write responses had deliberately been lost across the cases.

A further identical request in each case returned the exact saved JSON receipt;
accepted-write and attempt counters stayed unchanged. No local conversation,
inbox item, task or outbox delivery was created. Pending local intent was never
presented as a completed link before current IAM read-back succeeded.

## Review, archives and cleanup

The QA harness passed a three-pass independent review loop. Verified fixes loaded
the separate authoritative IAM client's PAT before reads, retained both streams
of container logs, and compared full Docker IDs during cleanup. These were fixed
before runtime execution. The three live scenarios passed on their first exercise.

Both databases were dumped in custom format and their archive contents validated.
Hashes, four merged service logs and any setup diagnostics are retained under
.cache/shuttle-work/prelink-remote-write-v1-qa/. Private configurations, assertions,
keys, dumps and logs must not be published. The peer never persisted Authorization
headers or raw assertion bodies. After ownership/image checks, all four containers,
the owned database volume and network were removed. Primary preservation checks
passed again after cleanup. The Git index stayed unchanged; no commit or push
occurred.

## Remaining gates

This closes the backend's accepted-write/interrupted-read-back gap for the sampled
paths. The provider hook's full retry/relay behavior, activation of the main
provider/backend, real Telegram proof, browser CA trust and final IAM session-cookie
relay remain separately bounded by their earlier reports and the deferred joint
interactive check. This is not full bidirectional browser-login or production
acceptance. It adds no new IAM linking policy and does not enable prelink on the
primary.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
