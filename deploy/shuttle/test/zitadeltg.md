# Owned zitadeltg runtime

This optional Linux service is for `shuttle-live-test`. It reuses the owned
[browser gateway](browser.md) at the canonical IAM origin. It does not create
an IAM provider or enable email auto-linking. The recorded baseline image has no
trusted prelink hook. An optional [prepared hook](../../../docs/shuttle-zitadeltg-prelink-provider.md)
is reviewed separately. Keep external registration disabled until the separate
real-Telegram proof and final IAM session-cookie relay gates pass; synthetic
combined-fixture acceptance does not establish those gates.

## Build input and credentials

Use a recorded clean commit from the sibling `zitadeltg` repository. Export only
`go.mod`, `go.sum`, `cmd`, `internal` and `LICENSE` into a new ignored directory;
record the commit and hashes of exported bytes. Do not build from the sibling
working directory or modify it. Build with this directory as context and the
absolute path to `zitadeltg.Dockerfile`. Record the resulting image ID and use
that exact ID as `TEST_ZITADELTG_IMAGE`; the overlay never pulls an image.
The build uses the same pinned Go1.27.2 toolchain as Zinusya and a scratch runtime.
The sibling project is Apache-2.0 licensed; its license and Go notices are
included. This local test image is not a production distribution or dependency
audit of all transitive modules.

`ZITADELTG_WEB_LOGIN_TOKEN` is `CLIENT_ID:CLIENT_SECRET` from BotFather's **Web
Login** configuration for the test bot. It is not implicitly the Bot API token.
Keep it in ignored local configuration and never print resolved Compose output.
A clearly marked synthetic secret can exercise runtime/JWKS/login rendering,
but cannot establish a successful Telegram authentication.

## Start and inspect

Append `zitadeltg.compose.yaml` to the current owned Compose model, retaining
resource, image and browser overlays. Resolve the model privately, verify it
only adds this provider and its volume, then run `--profile identity up -d
--no-deps zitadeltg`. No host port is published. Gateway routing is already
reserved at `/tg`; the provider joins only the browser network and trusts only
its fixed gateway hop172.29.253.2. Its callback uses the same canonical gateway
hostname and port as IAM. Public local CA trust is mounted for upstream HTTPS;
CA private keys and bot business credentials are not mounted.

Probe `/tg/healthz`, `/tg/keys` and `/tg/login/6087685431` over gateway HTTPS.
A health response is not proof of callback/linking behavior. Wrong bot IDs and
invalid callback proof must be rejected. Record the public JWKS before and after
a provider restart; it must remain identical. The signing key lives in a dedicated
owned volume, created by UID1000 with0600 permissions. Do not rotate it on restart,
copy it to documentation or mount it into other services. Removing the service
must preserve this volume. Removing this stand's keys is a separate destructive
reset, never a normal stop/cleanup step.

The inherited provider gives its HTTP server up to10s to drain, whereas Docker
allows at most5s here. This overlay enforces the kill bound; it does not claim
one-second graceful completion for the provider. Hardening its active callbacks
and restart recovery remains part of identity integration acceptance. Zinusya's
own drain/watchdog requirements are separate.

## Recorded native acceptance

The isolated source commit `f6c532e15adb26053897858a0f709e08b1e8591f` built with
Go1.27.2 into image
`sha256:02406dd496deaaa22d50b282ec96f09ff48c4cd67fa6ad29347f23eada08eb5a`
(16,010,690 bytes). Native gateway HTTPS checks passed for health, JWKS and login
rendering; wrong bot ID and malformed callback were rejected. The public JWKS
remained identical after restarting the same provider container. Its persisted
RSA key had0600 permissions and UID/GID1000. Existing business hashes/counts and
all companion container identities/start times remained unchanged.

Only a clearly synthetic Web Login secret was used; no real Telegram proof or
IAM registration/linking was exercised. The provider container was subsequently
stopped cleanly with exit0 and removed. Its signing volume and image remain.
Observed healthy restart/stop took about0.52s each, not a worst-case drain proof.
Ignored source/evidence receipts live under
`.cache/shuttle-live-test/zitadeltg-runtime/`; keep signing material private.
Both runtime configuration and the one-shot QA runner passed independent textual
reviews without actionable findings. The sibling checkout remained unchanged.

## Remaining identity gates

The recorded baseline provider verifies Telegram OIDC and relays a signed assertion
but has no trusted Zinusya prelink hook. The separate patched provider has passed
[combined provider/API/real-IAM fixtures](../../../docs/shuttle-prelink-combined-qa-2026-10-16.md)
for provider-first and Mini-App-first identity convergence, explicit opaque-subject
linking, lost-response retry, conflicts, revocation and completed-login restart.
A [resilience fixture](../../../docs/shuttle-prelink-resilience-qa-2026-10-16.md)
adds concurrent first logins and actual IAM stop/recovery. The separate
[backend remote-write gate](../../../docs/shuttle-prelink-remote-write-qa-2026-10-19.md)
checks recovery after IAM accepts account/link writes but responses or read-back
are unavailable, including graceful bot restarts and original-receipt replay.

These fixtures use synthetic Telegram proofs or assertions; the combined fixture
also uses a synthetic final IAM relay. They do not establish real Telegram login
or final IAM authorization/session-cookie relay. Main provider/backend activation
and user-assisted browser acceptance remain open. Keep email auto-linking disabled;
never infer an opaque Telegram OIDC subject from the numeric Telegram ID.

Real browser CA trust, Web Login origin approval, Telegram-issued proof, final
IAM session-cookie relay and bidirectional browser-login acceptance remain the
deferred joint gates. No production readiness or deployment is accepted.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
