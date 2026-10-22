# Current provider and Zitadel 4.20 prelink QA, 2026-10-22

The current bot and refreshed patched provider passed full OIDC/prelink on an
isolated copy of an owned synthetic IAM database with Zitadel 4.20. This extends
the [published-image compatibility](shuttle-iam420-compat-qa-2026-10-22.md) and
[existing-database upgrade](shuttle-iam-existing-upgrade-qa-2026-10-22.md) gates.
Production and the primary/base IAM installations were unchanged.

## Inputs and accepted behavior

- Bot source `be55b4a1`, `mvp-92/schema72`, image
  `sha256:7e0fc10b5b189d0a6ba76cf166bdd62e856f5e822cdabc569baef3fe41be8f83`.
- Patched provider based on upstream `59ac6a152e04dd6af0b10968dc67b10126921bb6`, image
  `sha256:858d7005772e8e5ca538983b0d866baec279024866ae2c25a8189f8126905084`.
- Published Zitadel 4.20.0, image
  `sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`.

The private clone reused an already retained synthetic IAM dump, issuer, master
key, admin PAT and fixture CA. It created a separate organization, scoped service
actor, OIDC/API clients, non-superuser application database and JWT provider.
Automatic IAM user creation was disabled: account creation/linking used the
bot's prelink API. TLS verification stayed enabled. Only the public gateway
listener was mapped to loopback; private prelink and synthetic JWKS routes were
not host-published. No real Telegram proof or paid model was used.

Provider-first login reached the final authorization callback without an IAM
registration/link form, exchanged the authorization code with PKCE S256 and
verified the ID-token signature, issuer, audience, nonce, expiry and subject.
Mini App bootstrap and a fresh provider login reused that same canonical bot/IAM
identity and its single external link. A second user completed Mini App first,
then provider login with the same identity. Stopping and starting the same bot
and provider containers preserved identity on another full provider login.

Final state contained two local users, three users in the scoped IAM organization
including its machine actor, five completed prelink receipts and no incomplete
prelinks. One receipt came from an earlier failed final relay described below;
it was retained, not erased. There were no bot tasks. The synthetic HTTP peer
received only readiness/JWKS reads, with no Loom or Telegram API calls.
Custom-format IAM and application backups were produced and decoded successfully.

## Failed attempts and fixture corrections

The original attempts remain failed. Continuations used the same cloned database
and retained their receipts; restore and resource creation were not blindly
replayed. Independent static reviews preceded each continuation.

1. Traefik rejected a file-provider mount named `routes.json`. The same JSON
   content was mounted as `routes.yaml`, a supported file extension.
2. Discovery returned 200 before the Management API's internal connection was
   ready. An organization-create request returned 503. A read-only diagnosis
   confirmed that organization was absent; readiness now waits for authenticated
   Management API access and rechecks the complete organization list before one
   fresh create. The observed startup statuses were 503, then 200.
3. The cloned instance retained Login V2 and redirected to an unserved
   `/ui/v2/login/login` route. `ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED`
   does not rewrite an existing instance feature. Only this clone was switched
   to Login V1 using `PUT /v2/features/instance`; its feature readback confirmed
   the change.
4. Docker assigned a different gateway address after a stopped-container restart.
   Prelink completed, but IAM denied its subsequent provider JWKS fetch. A
   read-only database check found one user and one completed prelink. The next
   continuation preserved and verified that identity, recomputed the default
   outbound denylist with only the current gateway `/32` excluded, and set
   provider proxy trust to that exact address. IAM/provider were recreated with
   the same database and signing-key volume. No broad private-network exception
   was added.
5. Simultaneous fixture shutdown stopped PostgreSQL before the bot finished its
   worker drain; that bot exited 1 and the attempt stayed failed. Final cleanup
   stopped bot/provider, then peer/IAM, gateway and PostgreSQL. Review also fixed
   cleanup so a partial container-recreation failure cannot skip stopping the
   remaining owned services merely because fewer than six containers survive.

The final continuation passed. All six fixture containers stopped with exit 0
and no OOM; retained volumes and backups permit follow-up without rerunning setup.
This fixture result does not establish the bot's shutdown timing under every
workload or accept arbitrary dependency shutdown ordering.

## Evidence and remaining gates

Ignored evidence is under
`.cache/shuttle-work/prelink420-current-20261022-v1/`: original receipts,
`r2/` through `r4/` failures, read-only diagnoses, and the successful
`r5/checkpoint.json`, image/state inspections, logs and private backups.
Never replay these one-shot setup or login controllers to inspect state.

The published unpatched provider still rejects prelink configuration. This gate
qualifies our patched candidate only. The subsequent
[current identity-client gate](shuttle-iam420-credential-expiry-qa-2026-10-22.md)
passes natural expiry and issuance recovery on the same IAM 4.20 clone.
Primary/base installation, current bot-task/Loom session renewal, user-assisted
real Telegram login and production readiness remain separate gates. No additional
human Telegram authorization was requested.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
