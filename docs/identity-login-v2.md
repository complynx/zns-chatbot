# Telegram identity with Login V2

For Telegram-only browser login with unverified synthetic email, use the matching ZITADEL and Login application **v4.16.3** images. Configure only this application's OIDC `LoginVersion.LoginV2.BaseUri` to the HTTPS Login application URL. Keep instance-wide `LoginV2.Required=false`. Set `EMAIL_VERIFICATION=false` on this dedicated Login application. The existing Telegram JWT relay works with the supported **`/idps/jwt`** callback.

This is a provider deployment configuration. It requires no runtime or relay protocol change and does not mark a mailbox verified. Independent Functional QA on 27 September 2026 verified this pinned local composition: actual PKCE exchange, bot-first/browser-first identity convergence, unverified email flags and opaque-subject isolation. The accompanying inactive-identity inbox fix passed separate Code QA and Functional QA. Production configuration and final real Telegram acceptance remain separate gates.

## Public production configuration

1. Use a normal HTTPS public hostname, for example `identity.example.org`, with trusted TLS. Route `/ui/v2/login/*` to the matching Login application, `/tg/*` to the Telegram relay, and ZITADEL's other paths to ZITADEL. Preserve the externally visible host and HTTPS scheme. Using one public origin for Login and relay also preserves the relay's `form-action 'self'` policy.
2. Create a separate Login application service account with **IAM_LOGIN_CLIENT** and a protected PAT. Mount it only into the Login application. Do not use the runtime actor, provisioner, bootstrap administrator, or bot process for this credential.
3. Set Login application variables:

   ```yaml
   ZITADEL_API_URL: https://identity.example.org
   NEXT_PUBLIC_BASE_PATH: /ui/v2/login
   ZITADEL_SERVICE_USER_TOKEN_FILE: /run/secrets/login-client.pat
   EMAIL_VERIFICATION: "false"
   CUSTOM_REQUEST_HEADERS: Host:identity.example.org,X-Forwarded-Proto:https
   ```

4. Set the bot/browser OIDC application's Login V2 base URI to `https://identity.example.org/ui/v2/login/`. Keep its existing authorization-code, PKCE, token-exchange, redirect URI and audience configuration. Do not switch unrelated applications to V2. A separate Login hostname must be registered as an instance Trusted Domain; forward `x-zitadel-public-host` and `x-zitadel-instance-host` as documented.
5. Configure the Generic JWT identity provider with the existing relay issuer and JWKS, and its `/tg/login/<bot-id>` authorization endpoint. The relay's `zitadel.jwt_endpoint` must be `https://identity.example.org/idps/jwt`; its redirect-origin allowlist must contain the Login application's HTTPS origin. Keep `synthetic_email_verified: false`.
6. Keep each bot's signed provisioning callback `identity_backend_url` pointed at the core's `/internal/identity/authorizer`. Retain separate **ORG_USER_MANAGER** provisioning credentials and **ORG_END_USER_IMPERSONATOR** runtime actor credentials. The existing instance impersonation policy is required for bot token exchange; Login V2 does not replace that requirement.
7. Configure an external-provider-only login policy for the intended organization as required by the deployment. Keep automatic provider-side account creation disabled when the core is responsible for reserving users. The core must complete the exact external link before the relay sends its JWT to ZITADEL.

`EMAIL_VERIFICATION=false` controls a login step for users of this Login application; it does not change ZITADEL's stored email verification flag. Do not reuse this Login application for flows that require verified email without reviewing that policy boundary. The dedicated app-only route keeps unrelated applications on their existing login configuration.

## Verification contract

A fresh trusted bot contact creates one ordinary local user and one reserved provider subject. Browser login subsequently links the validated opaque Telegram OIDC subject to that same provider account. A fresh browser-first login creates the same binding through the signed core callback, then bot contact resolves that person. Repeating either flow must preserve the local owner and provider subject.

Verify the actual authorization-code callback and PKCE token exchange. Read provider `human.email.isVerified` after login and require **false**. In the tested v4.16.3 response, userinfo omitted `email_verified`; an omitted claim is not evidence of verified email and must not be interpreted as true. Check exact local/provider external links as well as successful bot replies.

## Local stand accommodations

These are test-environment details, not production settings:

- Exact v4.16.3 Login source constructs an HTTP identity-provider return URL when the public host contains `localhost`. A TLS loopback IP or a dedicated test hostname avoids that development behavior. Do not relax the relay's HTTPS redirect checks.
- A synthetic provider may need a narrowly scoped outbound-network exception to reach its own private JWKS fixture. The proof allowed only `127.0.0.1`, preserving all other default denied networks. Production public JWKS needs no loopback exception. Never disable the full denylist as a deployment shortcut.
- Test TLS trust must be installed or passed explicitly to each service. Browser `ignoreHTTPSErrors` is only a local test accommodation.
- Local reverse proxies must forward gRPC HTTP/2 trailers and remove connection-specific HTTP/1 headers. Preserve the public host and TLS scheme.

## Version evidence

The general hosted-login page currently lists Generic JWT as unsupported, while the dedicated JWT provider guide documents V2's `/idps/jwt`. Exact v4.16.3 source registers that route and handles the JWT identity-provider intent. A real browser proof with the unchanged relay confirms the route works for this composition; the skipped upstream generic-JWT acceptance-test placeholders were not treated as proof.

- [JWT identity provider guide](https://zitadel.com/docs/guides/integrate/identity-providers/jwt_idp)
- [Login application configuration](https://zitadel.com/docs/guides/integrate/login-ui/login-app)
- [Adopt Login V2 per application](https://zitadel.com/docs/self-hosting/manage/adopt-login-v2)
- [Login client service account](https://zitadel.com/docs/self-hosting/manage/login-client)
- [General hosted-login limitations](https://zitadel.com/docs/guides/integrate/login/hosted-login)
- [v4.16.3 JWT route and intent handler](https://github.com/zitadel/zitadel/blob/v4.16.3/internal/api/idp/idp.go)
- [v4.16.3 email verification step](https://github.com/zitadel/zitadel/blob/v4.16.3/apps/login/src/lib/verify-helper.ts)
- [v4.16.3 localhost return URL behavior](https://github.com/zitadel/zitadel/blob/v4.16.3/apps/login/src/lib/server/idp.ts)
