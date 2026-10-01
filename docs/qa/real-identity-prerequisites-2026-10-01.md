# Genuine local identity qualification prerequisites

Lead setup receipt and qualification plan, 2026-10-01. Root subsequently authorized
the exact isolated two-service setup described below. Actual infrastructure is
running; runtime client/policy bootstrap is blocked before execution by automatic
approval review. No product/integration/FQA tests or application/LoginV2 services
were started. Final source and independent runtime/FQA gates remain separate.
Claude was not invoked/probed.

## Actual setup receipt

Project synthetic-qa-zns-identity-20261001 is solely owned by FQA lead. Both initial
services are running and retained with restart=no. PostgreSQL authenticated
readback reports17.11; native TCP127.0.0.1:58501 succeeds. Zitadel --version reports
v4.16.3; native TCP8113 and HTTP/debug/ready return success/200. Actual discovery
issuer is http://localhost:8113 with same-origin token/introspection endpoints.
This establishes infrastructure readiness only, not adapter or business acceptance.

| Actual resource | Handle and binding |
| --- | --- |
| PostgreSQL | container1501241cbbee1defbfe9a750a66b5ccbfec93326642bc8af5ac5195e89999a06; loopback58501; 1CPU/768MiB/256MiBshm |
| Zitadel | containerf42255fc25e5dac28f8609e83059a073669be407ba9887cd383c7b0947df2556; loopback8113; 2CPU/2GiB/pids256 |
| Pinned PostgreSQL Config.Image/imageID | postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24; same actual imageID |
| Pinned Zitadel Config.Image/imageID | ghcr.io/zitadel/zitadel@sha256:f3738fd984131d3f02e386d37fa480d1a30e42b7d4202e2b322f12f7cf556b64; same actual imageID |
| Private network | prefix_private; f7cb080e968fdf803f361068077740c0c0c6e84c22b45e290ac28a8b22f20f36; internal |
| Front network | prefix_front; 0b5c08f814de55a9ba0b803f149715f551903d8db2586ecc2e8cdf9ad117b3cc; bridge |
| Volumes | prefix_pgdata; prefix_pgsecret; prefix_bootstrap, where prefix is the exact project above |

Only these services/networks/volumes were created. Port and namespace collision
checks immediately before creation were empty; capacity32CPU/63,006,920,704bytes.
No foreign network membership, existing stand mutation, production/external
credential access or product schema/test changes occurred. Zitadel initialization
created only its own provider database/schema and bootstrap machine. Provider
database zns_identity_qa currently has owner postgres, as actual readback reports;
the runtime configured provider database user is zns_identity_owner. This is not
the product database and never becomes TEST_DATABASE_URL.

Generated local-only secret custody: ignored
qa.local/identity-prerequisites-20261001/private, protected Windows ACL with only
the current creator/owner COMPLYNX_FLOW\\ddriz FullControl. No passwords/PATs/keys
were printed, put in command arguments, tracked files or inspected environment.
Actual pinned-image passwd metadata proves PostgreSQL70:70 and Zitadel1000:1000.
Linux pgsecret directory700/password600 owned70:70; bootstrap directory700 and
config.yaml/steps.yaml/masterkey/admin.pat600 owned1000:1000. Fresh bootstrap PAT
was produced in the new instance's private volume; it has not been copied/read
for API bootstrap. Master key is32 newly generated ASCII bytes. Passwords are
independently generated cryptographic bytes. Exact generated expiry and file hash
receipts are in ignored publicreceipts/setup.json and private-custody.json.
Infrastructure JSON SHA256:
61B76F80A79F1629359B7D6EC3DF0F568CF27634B7C5560D244C38BA79C9AE82.
FORMAT keys only: postgresAdminPassword, postgresOwnerPassword, masterkey,
patExpiresUtc. No runtime state.json yet; unchanged real tests cannot run ready.

Receipt paths: qa.local/identity-prerequisites-20261001/publicreceipts contains
actual image passwd, file-mode, discovery, version, ready, authenticated PG,
resource/mount/limit and ACL/hash readbacks. setup.ps1 and compose.yaml are ignored
owned setup inputs. Pinned official v4.16.3 configuration/proto sources were read
to specify requests, rather than guessing current API contracts.

Automatic approval review rejected setup.ps1 -Bootstrap before execution:
"Although the local IdP bootstrap is bounded, it persistently enables
impersonation/security-policy behavior and creates privileged identity resources;
the user authorized broader QA expansion, not this exact security-setting change
or its blast radius." No bypass, indirect execution or rejected API mutation was
attempted. The rejected phase would create new synthetic runtime clients/humans,
ORG_END_USER_IMPERSONATOR actor, ORG_USER_MANAGER provisioner and enable
impersonation only in this newly owned instance. Root was notified of the exact
block. Explicit scope evidence or human confirmation must resolve this review
before that phase; healthy services are retained meanwhile. Bootstrap is not
complete, and no runtime client or human identity readiness is claimed.

Lead retains setup/lifecycle ownership pending this block. Root may inspect
sanitized receipts. No qualification runner owns the provider yet, and no rebuild,
restart or credential mutation is authorized during a later runner freeze.

## Actual inventory and bounded allocation

At the original read-only inventory, Docker had no cached Zitadel image or
existing Zitadel container. Root then acquired the official immutable4.16.3
image recorded above. Historical compose.identity.yaml is reference only; none
of its fixed credentials, dates, namespace or volumes were reused.

Reusable cached PostgreSQL17.11: repository@sha256
b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24, actual image
ID matches. Existing PG containers include native55432/58441/58451/58461/58471
and historical functional stands; none is an identity target. Docker capacity
32CPUs/63,006,920,704bytes. There are no current native listeners on8113 or
58501-58504 and no matching proposed Docker namespace/network/volume. Repeat full
inventory/capacity checks immediately before approved creation; absence now is
not a permanent allocation guarantee.

Propose project synthetic-qa-zns-identity-20261001, only two initial services:

| Resource | Proposal |
| --- | --- |
| PostgreSQL | project-prefix-postgres-1; own-prefix-pgdata volume; 1CPU/768MiB/256MiBshm; restart=no |
| Zitadel | project-prefix-zitadel-1; actual4.16.3 repository@sha256 once acquired; 2CPU/2GiB/pids256; restart=no |
| Network | project-prefix-private plus dedicated-front bridge; no foreign network membership |
| PostgreSQL host transport | 127.0.0.1:58501->5432; provider database zns_identity_qa, separate newly generated database-owner credential |
| IdP native issuer | http://localhost:8113, publication127.0.0.1:8113->8080 |
| Initialization custody | separate own private bootstrap volume/directory and newly generated master key/admin PAT; never old qa.local/identity |
| Future native runtime reservations | 127.0.0.1:58502 API/app,58503 functional UI,58504 optional control; not created by two-service setup |

Hardcoded issuer8113 in both unchanged live tests prevents arbitrary issuer port
substitution. This does not conflict with584xx or native8090. If8113 becomes
occupied, report the conflict; never stop that owner or silently edit test source.
Native proof runs on the host. Container app localhost points at itself: managed
runtime must separately prove an issuer-consistent route, not merely change URL
to an internal hostname that changes claims. First two-service setup does not
certify container runtime routing or a browser LoginV2 deployment.

The historical platform/compose.yaml uses55432/8090/8091 and public literal
sandbox credentials; compose.qa.yaml exposes API8091. Those files identify old
contracts, not an instruction to launch/reuse them. Do not copy their namespaces,
data, credentials, unlimited resources or restart policy into this allocation.

## Unchanged real tests and private format

TestZitadelLocalAdapter in internal/identity requires ZITADEL_LOCAL_STATE, private
JSON keys project.id; bot_app.clientId/clientSecret; api_app.clientId/clientSecret;
actor_secret.clientId/clientSecret; actor.userId; alice.userId; bob.userId. Exact
issuer is http://localhost:8113, sandbox HTTP explicitly enabled. It calls real
OAuth exchange for Alice/Bob and authenticated introspection, checks delegated
subject, and denies nonexistent subject, invalid token, actor/API credentials,
audience and actor claim. It skips when state is absent. No values are read here.

TestSDKLocalZitadelProvisioning in identityprovision requires
ZITADEL_PROVISIONING_PROOF_STATE with org.org.id and sibling admin.pat. It creates
one uniquely named synthetic SDK user, reads exact active/human/organization/
operation metadata, unverified email and empty external links, rejects conflicting
creation, and deletes only its own provider ID. This test's admin PAT belongs only
to the new local synthetic instance and never runtime bot/model. Its unchanged
proof is not a complete minimum-role demonstration. Separate scoped provisioner
qualification must use ORG_USER_MANAGER, without IAM administration in runtime.

New ignored custody proposal: qa.local/identity-prerequisites-20261001/private/,
protected owner-only ACL. Separate generated IdP master key, provider-PG admin/
application password, bootstrap admin PAT, project bot/API confidential clients,
organization impersonator client and organization provisioner client/token.
No production/provider credentials read or reused. Exact file paths/hashes,
format KEYS and creation provenance are public; values, environment dumps,
PATs, assertions and tokens never enter output/argv/Git. Linux secret files use
actual service UID/mode proof, not assumed Windows chmod. Expired bootstrap PAT
must be replaced by a newly generated synthetic bootstrap credential, not an old
account token. Use owner-approved runtime helpers to avoid secret-printing probes.

Bootstrap via current pinned supported API after real health/discovery: one local
organization/project; separate bot token-exchange and API-introspection clients;
actor service account with ORG_END_USER_IMPERSONATOR; required instance token-
exchange impersonation policy; scoped provisioner ORG_USER_MANAGER; two ACTIVE
human users with random initial passwords/unverified synthetic invalid-domain
email and no external links. Actor uses audience-scoped client credentials;
runtime PAT substitution does not meet the intended actor contract. Do not grant
general IAM roles to bot/actor/provisioner or elevate them to hide a failure.

Reviewed root identity template setting names include external domain/port/secure,
Postgres host/port/database/admin/user SSL modes, first-instance organization and
machine PAT path/expiry. Preserve version-specific init contract; render new
private values through a reviewed private-file mechanism. Historical fixed key
or dates are not fresh credentials. Acquire actual digest/start/bootstrap only
after root approves exact template/helper paths and sole setup writer.

## Proof matrix and limits

Current integration/zitadel_* tests use deterministic runtimeProvider or httptest
OAuth/introspection providers. They cover owner/ACL composition, cache invalidation,
fairness and outages, but cannot establish genuine Zitadel wire/runtime behavior.
Keep them unchanged in the full test union and separately execute real qualification.

| Requirement | Actual qualification needed |
| --- | --- |
| Token wire and claims | Native unchanged real adapter test, separate bot/API/actor credentials, active/issuer/audience/client/expiry/not-before/actor validation |
| Bounded cache | Exchange deadline min(5min,90% provider lifetime); verify deadline min(5min,token expiry), bounded positive cache and no negative-cache substitution; actor renewal90%, renewal failure rejects |
| Local targeted revocation | Authoritative inactive exact mapping invalidates that subject's exchange/introspection caches across composed adapters; unrelated subject remains available; runtime ACL rechecks already-resolved context |
| Disabled provider human | Genuine IdP disable/readback + fresh or expired-cache exchange/introspection denial; never reactivate/rebind; bounded cache may retain prior verdict until its declared deadline |
| Bot-first/auth-first | Same durable numeric bot/user reservation converges to one owner/provider ID across both trusted routes; real provider SDK readback plus fresh UI/runtime proof, no email adoption |
| Exact external mapping | Bind bot_id/telegram_user_id to exact provider user; authorizer OIDC sub remains distinct, immutable reserved IdP/external subject tuple, exact SDK link readback/conflict denial |
| Import preservation | Independent explicit mapping attestation/plan hashes and existing user/profile/link/eligibility preservation; unknown mappings fail closed, no name/email inference |

Cache tests prove a fixed five-minute bound and targeted local invalidation;
the live adapter test alone does not wait through expiry or exercise the local
mapping layer. Genuine cache qualification needs a source-frozen instrumented
runtime or approved black-box observations, actual expired-token/provider outage
and local revocation windows. Do not shorten production cache settings, use a
mock identity to stand in for the provider, or infer fresh revocation merely from
an already-cached token. Tokens/body/private claims are not reviewer evidence.

Source limits:1024 cached credentials/concurrent lookups per positive cache,
10-second lookup/HTTP deadlines, no hit/outage deadline extension, token length
at most16KiB and subject at most256 characters. Actual overload/concurrency and
targeted invalidation proof must preserve these limits rather than raise them.

Runtime provisioning remains opt-in: enabled, organization_id,email_domain,
client_id/client_secret on app/core; bot retains trusted bot ID. The internal
telegram onboarding operation uses its existing service signer, separate audience,
one-minute lifetime/body digest. Ordinary bearer/model cannot onboard or choose
principal. Disabled existing users are refused; imported can_book policy is not
reset. Exact provider reservation survives ambiguous create before local commit.

Authorizer path requires configured issuer/JWKS/idp_id, trusted RS256 assertion
with exact issuer, sole zns-identity-provisioner audience, purpose,≤60s lifetime,
jti and distinct canonical bot/user/OIDC subject claims. For true browser flow,
additional reviewed LoginV2 v4.16.3 service, TLS/proxy and relay JWKS/principal
contracts are needed: application-only LoginV2, EMAIL_VERIFICATION=false,
instance Required=false, /idps/jwt relay and synthetic_email_verified=false.
Two-container IdP setup alone does not supply these; no Login image is currently
confirmed cached. Full real Telegram login waits on the designated recipient ID.

## Responsibilities and readiness sequence

1. Root approves bounded image acquisition and exact new setup paths; infrastructure
   lead/engineer is sole writer. Final product epoch remains independent.
2. Setup verifies actual image digest/version, network/volumes/loopback transport,
   PostgreSQL roles/IdP health and issuer discovery. Newly generated bootstrap
   uses only this instance; record sanitized object IDs/private file hashes.
3. Create clients/actor/users and scoped provisioner through genuine APIs; verify
   role sufficiency and negative denials. Freeze config/instance handles and release
   setup; no service restart/key/client change while qualification owns stand.
4. Root assigns exclusive native proof runner for both unchanged live tests;
   preserve raw results/skips, exact source/environment keys and provider version.
5. Separate fresh runtime/FQA owner executes cache/local revocation/onboarding/
   exact links and eventual browser/import cases against reviewed product/UI,
   with dedicated product DB/role namespace if runtime materialization is required.
   Provider datastore never becomes TEST_DATABASE_URL for broad Go tests.

Image acquisition, issuer routing, private file custody and synthetic setup are
engineering dependencies. The new local instance's scoped identity creation and
impersonation policy are now a concrete automatic-review block: root will request
exact human authorization before bootstrap proceeds. This supersedes the
original plan's assumption that broader QA authorization was sufficient for that
phase. The designated real Telegram account ID remains a separate human dependency
for actual external messaging/login. No production cutover, token rotation or
external authorizer change follows from local stand authorization. No FQA PASS,
adapter acceptance, final integration or complete runtime readiness is claimed.
