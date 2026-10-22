# Zitadel 4.20: changed-path qualification, 2026-10-22

The upgrade does not require repeating the complete bot acceptance suite. This
review compares upstream `v4.16.3` to `v4.20.0`, maps changes to the actual bot
integration and reuses the accepted exact-image results. Two previously uncovered
authorization boundaries received new isolated runtime checks.

The complete [upstream diff](https://github.com/zitadel/zitadel/compare/v4.16.3...v4.20.0)
contains 909 changed paths. The retained diff SHA256 is
`ee088af04a92e384eba9bfdf6ecc09179c4f5914266ef1bf1af255ab066ce79a`.
The GitHub compare connector returned only its first 300 paths; classification
uses the full downloaded diff, including backend files beyond that limit.

## Changes and sufficient evidence

| Change relevant to this integration | Qualification |
| --- | --- |
| Token exchange distinguishes an IAM administrator from an ordinary user and requires `admin.impersonation` for the former (`token_exchange.go`, `oidc_session.go`, new `user_is_admin.sql`). | New runtime assertions: ordinary impersonation succeeds; an actor with only `ORG_END_USER_IMPERSONATOR` receives HTTP 403 / `invalid_request` for an IAM member; explicit `ORG_ADMIN_IMPERSONATOR` allows the same subject. |
| Inactive organizations block issuance, userinfo and token/session authorization; refresh grants retain deactivation history. | New runtime gate: ordinary user token works initially; after its new organization's deactivation, exchange returns HTTP 400 / `Errors.User.NotActive` and the still-unexpired token's userinfo returns HTTP 401. Reactivation permits a fresh exchange for the same subject. The bot does not use OAuth refresh-token grants. |
| Direct external-identity linking now requires user-write permission even for self; the verified Login V1 path remains separate (`user_idp_link.go`). | The current service-side prelink and complete provider login already passed on exact IAM 4.20. Retain that positive evidence. No new raw self-link API acceptance is claimed. |
| Event projection cursor ordering, unique-constraint ownership and setup migrations changed. | Reuse the accepted existing-database 4.16.3→4.20 upgrade, original event/user/grant preservation, restart and backup restore. The earlier repaired fresh fixture retains its distinct failure; no general projection-completeness claim follows from it. |
| Exact email/login search uses new index-seek queries; OIDC route registration changed alongside dynamic client registration. | Reuse current Mini App identity lookup/profile sync and full prelink/OIDC/PKCE/ID-token checks in both login orders. These exercise the used paths; no unrelated search benchmark or DCR suite is needed. |
| Go/TLS/URL behavior changed, including outbound trust restrictions described in the [4.20 release notes](https://github.com/zitadel/zitadel/releases/tag/v4.20.0). | Current full OIDC/prelink exercised the configured TLS provider path. Production SMTP, Actions and other external endpoints have their own configuration scope; no blanket claim about them is made. |

The bot's ordinary token acquisition, local cache/fallback and Loom control flow
do not become new acceptance tasks solely because the IAM version changed.
Token exchange itself did change, as listed above. Its organization-role filter
downscoping is outside current `identity.Client.scopes`: the bot requests neither
`urn:zitadel:iam:org:roles:id:*` nor refresh-token grants. Existing current-client
natural-expiry/outage/recovery evidence remains applicable. A complete running
Loom task/outage rerun is not an upgrade gate without a separate unmet requirement.

Login V2 device-code proofs, passkeys, native app links, dynamic client
registration and the new Zitadel-to-Zitadel provider are outside the configured
Login V1/static-client flow. Their changes do not justify retesting all bot
business scenarios.

## Runtime scope and preservation

The new checks use the retained, owned `zinusya-prelink420-current-v1` synthetic
clone with exact IAM image
`sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`.
Only its PostgreSQL, gateway and IAM start. The bot, provider and mock peer remain
stopped. Each check creates a new organization/user and adds only organization-
scoped membership for the fixture actor; cleanup removes that new organization.
Existing application users, prelinks and tasks retain identical hashes. All six
original fixture containers finish stopped, exit 0, without OOM. Original OIDC
settings are not changed. TLS verification remains enabled.

The administrator assertions passed in `r3` before a later controller assertion
failed on a localization key. The original `r3` receipt remains `passed=false`;
only its completed administrator assertions are reused. The separate `r4`
organization-only gate passes completely and does not repeat the administrator
test. The aggregate accepts these two stated boundaries, not the whole failed
`r3` controller.

Earlier controller failures remain recorded: initial startup returned a non-JSON
gateway response; `r2` incorrectly expected HTTP 400 for the actual permission
denial HTTP 403; `r3` expected translated prose instead of the returned
`Errors.User.NotActive` key. Their cleanup completed and application hashes stayed
unchanged. These are controller errors, not established product regressions.
Fresh independent textual reviews preceded effects and followed controller fixes.

Private evidence is under `.cache/shuttle-work/iam420-delta-20261022-v1/`:
`delta-map.json`, `acceptance.json`, original failed receipts, independent review
input hashes, and `r4/security-checkpoint.json`. No production data, real Telegram
authorization, model request, source-code fix or production mutation occurred.

## Release implication

An application role such as `zinusya.admin` is distinct from an IAM instance,
organization or project membership. Ordinary bot users and application-role
administrators do not need blanket administrator impersonation privileges.
If a canonical linked user is an IAM administrator, the new explicit permission
boundary must be handled deliberately in the deployment policy; this gate does
not grant the production actor additional privileges.

Primary/base activation, real Telegram consent, model onboarding and release
cutover retain their separate scopes. Historical upgrade acceptance is not
invalidated, and this report does not claim production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
