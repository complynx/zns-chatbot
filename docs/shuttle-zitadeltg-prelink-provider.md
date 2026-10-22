# Owned zitadeltg prelink hook

The optional per-bot hook is distributed as
[`zitadeltg-prelink.patch`](../deploy/shuttle/test/zitadeltg-prelink.patch) against
provider commit `f6c532e15adb26053897858a0f709e08b1e8591f`. The reviewed patch was
built into the [candidate provider image](shuttle-prelink-images-qa-2026-10-16.md)
and exercised by [combined fixtures](shuttle-prelink-combined-qa-2026-10-16.md);
main provider activation remains open. The sibling checkout is not modified. The [private backend contract](shuttle-telegram-prelink-api.md) remains
the authority for link persistence and current authorization.

## Intermediate real Telegram browser proof (2026-10-22)

An isolated local IAM/provider fixture reached ZITADEL's **External User Not
Found** form after a real Telegram login. This proves the baseline Telegram
callback, provider validation, IAM JWKS fetch and registration-form relay for
that attempt. It does not prove automatic account creation, prelink, an
authenticated IAM session or either direction of application-account linking.
The fixture still runs the baseline provider, without the prelink hook.
The required integrated flow must create or link the canonical account
automatically and complete authentication without displaying this form or
requiring manual Link/Register. The final browser gate remains open.

The fixture required these configuration corrections:

- Use the bundled Login V1 when no separate Login V2 service is deployed.
- Open Telegram Login over standard HTTPS443. Configure the test bot's exact
  redirect URI and trusted origin. The earlier nonstandard-port attempts
  authorized successfully in Telegram but ended with `popup_closed` locally;
  Telegram's public auth page derived an origin without that port.
- Permit IAM's JWKS fetch to the owned gateway's exact Docker IP. The fixture
  excludes that single `/32` from the default HTTP-client denylist, preserving
  every other denied address and TLS validation. Keep provider proxy trust
  synchronized with the gateway's exact IP after container recreation.

For this test only, the gateway exposes Telegram routes on loopback443 while
the IAM issuer remains on28443. Production uses its normal HTTPS origin and
does not require this alias. Safe gateway access logs omit headers, query
strings and bodies; provider debug is off because it logs unsigned JWT claims.
The disposable same-origin/cross-origin popup probe passed and was removed.
Private local receipts retain the original failures and the corrected attempt.

A later [synthetic-proof/real-IAM acceptance](shuttle-prelink-real-iam-qa-2026-10-22.md)
activates the hook in a separate scoped fixture and completes OIDC without this
form, in both identity orders and after restart. Its Telegram proofs are synthetic;
activation and final browser acceptance with the real Telegram account remain open.

## Behavior

After Telegram signature, audience, issuer, nonce and state validation, the hook
signs a dedicated RS256 prelink assertion using the provider's existing key/JWKS.
Numeric Telegram ID and actual opaque subject remain separate. The assertion
has a distinct audience/purpose and at most60s lifetime, bounded by the source
proof. One assertion/JTI/body is retained across at most3 bounded attempts.
The total budget is8s, individual requests at most3s; source expiry also bounds it.

The dedicated service bearer is sent only to the configured HTTPS endpoint.
Cookies and redirects are disabled. The receipt must match the assertion digest,
issuer/JTI, numeric/opaque identity, configured org/provider and exact expiry.
It must be completed, fresh and contain nonzero operation/user UUIDs plus an IAM
subject. Denied/conflicting, malformed, oversized, expired or mismatched receipts
withhold the IAM relay. The backend must freshly revalidate access even when
returning a persisted completion receipt.

Configuration is under each bot's `prelink`: `endpoint`, `audience`,
`service_bearer`, `organization_id`, `provider_id`. All empty disables the hook;
partial or unsafe values fail startup. Secrets support existing env expansion.
The dedicated key must be canonical base64url encoding of32bytes, distinct from
the bot secret; the audience must differ from the IAM relay audience. The exact
endpoint path is `/internal/identity/telegram-prelink` with no query/fragment.

For opted-in bots, the relay uses the verified reserved `telegram.invalid`
synthetic address, matching the prelink intent. Bots without the hook retain
their configured global email domain/verification policy. Enabling one bot must
not change another bot's claims or require changing the global email policy.
The relay JWT has its existing IAM audience and no prelink-purpose claim.

## Reproduce source

Run from the zns-chatbot repository root with Python3 and Git:

```powershell
python deploy/shuttle/test/prepare_zitadeltg.py --repository ../zitadeltg --output .cache/shuttle-live-test/zitadeltg-runtime/prelink-build-source
```

Choose a new output directory; existing output or receipt is refused. The helper
exports pinned Git blobs, applies the reviewed patch with LF handling independent
of `core.autocrlf`, and records base/patched hashes plus patch digest. It uses no
third-party Python dependency. Build using that prepared directory as context and
`deploy/shuttle/test/zitadeltg.Dockerfile` as the absolute Dockerfile path. This
step does not enable prelink, create an IAM provider or start a container.

## Component evidence and limits

The complete provider suite passed on Linux/Go1.27.2 under race:140 root tests,
296 test/subtest pass events, no failed or skipped test. The command package has
no tests. `go vet ./...` also passed. Focused `TestPrelink` tests passed on Windows;
the inherited full Windows suite is not accepted because six existing key-file
persistence tests require Unix permissions. No inherited test was disabled.

A two-bot callback regression first failed because prelink validation required a
global email-policy change. The fix makes that policy specific to the enabled
bot; a fresh independent review found no actionable findings. Callback fixtures
cover successful ordering, identical retry after a lost completion reply,
denial/outage, malformed/mismatched receipts, source expiry, cancellation and
invalid Telegram proof. These use synthetic signed Telegram proofs and HTTP
peers; they do not prove a real Telegram login or combined backend/IAM flow.

The first repeat Linux invocation failed before tests because CGO was disabled;
the corrected bounded runner installed the C toolchain and passed. The runner
exited0 with no OOM/cgroup memory events and was removed. The first source
reproduction exposed Git's Windows CRLF conversion; explicit LF handling then
reproduced all25 reviewed files byte for byte. These failures remain in ignored
`.cache/shuttle-work/provider-prelink-qa/` evidence. The final patch SHA256 is
`8f182973af64bfe25bf0feb3608aa125e1528d8c07a7d40d1905d46f32bd6197`.

[Private TLS route source and isolated preflight](shuttle-prelink-route-qa-2026-10-16.md)
are accepted, including owned gateway recreation. The [patched images](shuttle-prelink-images-qa-2026-10-16.md)
and [combined native provider/API/real-IAM fixture paths](shuttle-prelink-combined-qa-2026-10-16.md)
now pass within the stated synthetic-Telegram/final-relay boundary. A further
[resilience fixture](shuttle-prelink-resilience-qa-2026-10-16.md) covers synchronized
first logins and actual IAM stop/recovery. The [main bot image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md)
now passes with prelink disabled. The current [backend accepted-write recovery gate](shuttle-prelink-remote-write-qa-2026-10-19.md)
now covers the sampled remote-write interruption paths through the actual bot,
with synthetic provider assertions. Main provider/backend activation remains open. Browser CA trust,
real Telegram Web Login credentials/proof and user-assisted login are separate
interactive gates. No production readiness or deployment is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
