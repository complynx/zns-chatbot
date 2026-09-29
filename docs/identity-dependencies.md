# Identity dependency assessment

Pinned versions are in `platform/go.mod`. This record distinguishes selection evidence from release acceptance. Current vulnerability scans, real PostgreSQL checks, race checks, and independent QA are separate gates.

| Dependency | Pin | License | Selection and compatibility |
| --- | --- | --- | --- |
| zitadel-go/v3 | v3.29.2 | Apache-2.0 | Official generated UserService client; release targets Zitadel4.16.0. Exact create, metadata and external-link operations passed on local4.16.3. Newer API-targeting versions are not substituted automatically. |
| golang-jwt/jwt/v5 | v5.3.1 | MIT | Maintained JWT parsing and RS256 verification; avoids handwritten signature validation. |
| MicahParks/keyfunc/v3 | v3.8.2 | Apache-2.0 | Configurable JWKS fetch and refresh lifecycle; pins the JWT/JWK dependencies below and retracts affected old3.3.x versions. |
| MicahParks/jwkset | v0.11.3 | Apache-2.0 | Keyfunc's JWK validation/cache dependency and public fixture serialization. |
| envoyproxy/protoc-gen-validate | v1.3.3 | Apache-2.0 | Generated SDK message validation dependency. |
| golang.org/x/time | v0.15.0 | Go BSD | Keyfunc refresh rate limiter. |

The licenses were checked in the downloaded pinned module sources. The platform's Go1.27 satisfies the JWT/JWKS libraries' Go1.25 minimum. Upstream maintenance and compatibility evidence: [SDK release](https://github.com/zitadel/zitadel-go/releases/tag/v3.29.2), [SDK support policy](https://github.com/zitadel/zitadel-go), [pinned UserService API](https://github.com/zitadel/zitadel/blob/v4.16.3/proto/zitadel/user/v2/user_service.proto), [Keyfunc pinned module and retractions](https://github.com/MicahParks/keyfunc/blob/v3.8.2/go.mod), and [JWT release](https://github.com/golang-jwt/jwt/releases/tag/v5.3.1).

Downstream use is evidence of adoption, not a security guarantee. [Prometheus lists JWT v5.3.1](https://github.com/prometheus/prometheus/blob/main/go.mod). [GitLab Agent's tagged dependency file](https://gitlab.com/gitlab-org/cluster-integration/gitlab-agent/-/blob/v18.0.0-rc42/go.mod) lists keyfunc/v3 and jwkset, at older versions; it does not establish adoption of our exact pins. The SDK is the [vendor's documented Go integration](https://zitadel.com/docs/examples/login/go), and the [Go package index](https://pkg.go.dev/github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/app) records downstream importers of its generated API packages. Broad independent production adoption of the exact SDK pin was not established. We select it for the vendor API contract and verified local compatibility, rather than implementing the protocol ourselves.

Release acceptance must record the pinned `govulncheck` v1.8.0 runtime and SQLC-tool scans plus module verification for the actual frozen candidate. No advisory-clean claim follows from version numbers, license checks, import counts or source review alone.
