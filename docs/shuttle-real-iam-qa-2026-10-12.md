# Shuttle real IAM contract QA — 2026-10-12

This lane uses the actual Shuttle identity client and a disposable Zitadel
**v4.16.3** provider. JWT signature checks, discovery, JWKS, token issuance,
user provisioning and introspection use real HTTP responses. Telegram and Loom
are not involved in this lane. This report supplements the process-level manual
QA; it does not accept production or the complete business MVP.

## Stand

- Zitadel image: `ghcr.io/zitadel/zitadel@sha256:f3738fd984131d3f02e386d37fa480d1a30e42b7d4202e2b322f12f7cf556b64`.
- PostgreSQL image: `postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24`.
- Issuer: `http://localhost:18180`; PostgreSQL exposed only on `127.0.0.1:55475`.
- Owned resources: `shuttle-real-iam-id-20261012`,
  `shuttle-real-iam-db-20261012`, `shuttle-real-iam-net-20261012`.
- Two organizations, distinct Loom and Shuttle API projects, an impersonating
  machine, separate exchange and API-project introspection applications.
- Stand-only credentials and helper programs live in ignored
  `.cache/shuttle-real-iam-qa/`. No credential or complete JWT is recorded here.

The fixture was adapted from Loom's disposable extended IAM stand. Docker
assigned the network subnet after a fixed subnet collided with an existing
network. Zitadel was restarted after PostgreSQL became ready. Existing Docker
resources, sibling fixtures and production services were not changed.

## Acceptance status

**Final post-fix acceptance passed.** The renewed complete-source review and
documentation audit finished before this execution. All of the following probes
passed against the actual provider:

- Machine `client_credentials` and exact service subject/resource owner.
- Creation of a human with a caller-persisted proposed ID and verified
  `tg+123+457@telegram.invalid` email; repeated reconciliation with a different
  proposed ID returns the same existing account.
- User-ID token exchange with the actual user `sub`, service `act.sub`, configured
  organization and both distinct project audiences.
- JWT signature, discovery/JWKS and active introspection validation.
- An unassigned user has no application roles; foreign-organization and missing
  subjects are denied; a modified signature is denied.
- Explicit local invalidation removes the previous credential/check cache and
  permits a new delegation while IAM is available.
- With the actual IAM stopped, warm issuance and positive authorization caches
  still work within their existing validity. A new cold client returns
  `identity.ErrUnavailable`. Invalidation removes warm authorization and issuance
  access during the outage. Issuance recovers after the actual IAM restarts.
- A real five-second access token initially verifies, expires, is denied by a
  fresh client, and is replaced automatically by a new verified credential.

The final role lane also passed: a Shuttle-only `shuttle.admin` grant is returned
by the dedicated Shuttle introspection client; a Loom-only grant with the same
name is denied; wrongly configuring the Loom introspection client still does not
create Shuttle administrative rights.

The initial lane used synthetic user `456`; the final lane used a new roleless
synthetic user `457` to prove creation after the role-namespace fix. Actual user
`456` was retained for isolated role-grant controls.

An expiry-fixture settings request returned **HTTP 503** immediately after
provider restart, although discovery and normal token issuance were already
ready. The final lane waits for administrative settings readiness before setting
the five-second lifetime. The original failed attempt was retained and the full
lane then passed. This was a fixture timing correction; no product change was
made for that request.

## Role namespace finding and controls

The original fixture accidentally used an introspection application belonging
to the Loom project while the configured API audience belonged to Shuttle.
Under that configuration, a real Shuttle-only `shuttle.admin` grant was absent
from the flat role claim. A real Loom-only grant with the same name appeared in
that flat claim and the original client treated it as Shuttle administration.
This exposed a configuration-sensitive privilege boundary.

The provider's introspection application determines the returned project role
namespace in this stand. A dedicated introspection application in the Shuttle
API project returns Shuttle roles and denies the Loom-only collision. With
`urn:zitadel:iam:org:projects:roles` requested on fresh actor issuance and user
exchange, it returns the exact claim
`urn:zitadel:iam:org:project:{shuttle-api-project}:roles`. Reversing the repeated
audience parameter or omitting that parameter while retaining both audience
scopes did not change the introspection boundary.

The product fix requests that plural role scope and reads only the configured
Shuttle API project's namespaced introspection role claim. It does not fall back
to a flat claim from another project. The final live execution proved all three
required boundaries: the Shuttle-only grant succeeds, a Loom-only collision is
denied, and mismatched Loom introspection credentials fail closed.

Safe original and final output excerpts:

```text
Before the fix, API-only grant with mismatched Loom introspection:
FAIL actual assigned Shuttle project admin role missing
Before the fix, Loom-only grant with mismatched Loom introspection:
PASS actual assigned Shuttle project admin role present after JWT/JWKS/introspection

After the fix, API-only grant with dedicated API introspection:
PASS actual API admin role expectation; present= true expected= true
After the fix, Loom-only grant with dedicated API introspection:
PASS actual API admin role expectation; present= false expected= false
After the fix, Loom-only grant with mismatched Loom introspection:
PASS actual API admin role expectation; present= false expected= false
```

The pre-fix `PASS` above is the intentionally permissive role helper's output;
under the collision fixture it proves an incorrect administrative grant. It is
not a successful security acceptance result.

Provider references: [reserved scopes](https://zitadel.com/docs/apis/openidoauth/scopes),
[role retrieval](https://zitadel.com/docs/guides/integrate/retrieve-user-roles).

## Limits

This does not execute the bot process or Loom, prove restart recovery of tasks,
test Telegram browser login, or exercise the administrative HTTP handlers.
Those have separate process/mock-server acceptance coverage. The tested
impersonation policy and role assignments are disposable fixtures, not approval
to change production IAM policy.

Bidirectional provisioning with `zitadeltg`, production IAM credentials and
permission policy, and actual deployed Loom interoperability remain acceptance
gates. Matching the synthetic email alone does not prove bidirectional linking.
Warm Shuttle authorization caching does not prove Loom's own IAM outage behavior.

`EnsureUser` creates an identity without assigning project roles. In the sibling
Loom implementation, harness permissions such as `runs.create` derive from the
service actor and manifest binding; business and module permissions derive from
the user. A roleless new human is therefore not, by itself, a blocker for a base
agent run. Production must configure the actor's Loom role permissions and
service-subject classification or discovery. Valid user-ID exchange and audience
claims still do not prove an actual Loom round trip; that remains a separate gate.

## Cleanup

Only the two owned stand containers and their owned network were removed after
evidence collection. Stand credentials, issued tokens and the PAT were removed
from the owned scratch directory. Sanitized stdout and credential-free helper
sources remain in ignored scratch for local diagnosis. No production server,
production credential, paid model or sibling container was used or changed.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
