# Loom operation adoption: consumer acceptance

Acceptance date: 2026-10-21 UTC. Scope: the Zinusya knowledge module's original-operation handoff requested in [Loom issue #20](https://github.com/complynx/loom/issues/20).

## Versions and boundary

The disposable `kb-adopt-r2` stand ran Zinusya source `4f03e5ff7a0c8db0f322ebd7f22cdb275ac30b7b`, manifest `mvp-91`, schema 70 and image `sha256:a9c2d76e5f3a01889ef041e4bfba8131433a84241829e6cbd1656815e00869fd`. Its binary was built from that commit and layered onto the accepted runtime image; the unchanged media worker was reused.

Loom used the published revision `34f4646832748efd455828f6804c6274027db659`, image `sha256:003529b117602d52905e1d5f65d353fbe45e969dfc065eae638627b379f813ff`. Its API and run worker used this revision. No Loom checkout or developer stand was changed.

The test used actual PostgreSQL and Zitadel, two real fixture identities, authenticated Mini App HTTP requests, mock Telegram, and a controlled synthetic model provider. The provider supplied tool calls after ordinary Telegram text input. This proves transport, authorization, dialogue state and durable effects. It does not prove autonomous model interpretation, real Telegram WebView behavior or production readiness.

The primary stand stayed on schema 69 / manifest `mvp-89`; its domain data and runtime fingerprints were checked unchanged. This report does not install the candidate there.

## Runtime gates

1. A native form prepared a suggestion. Loom adopted that original backend ID into a distinct internal operation, preserving normalized arguments, description, preconditions and absolute expiry. No effect occurred before fresh plain-text consent. Approval produced one suggestion; native status returned the same receipt.
2. In the reverse flow, the agent prepared an operation and the native client confirmed it first. The backend receipt existed while the Loom operation was still prepared, before any Loom consent. Subsequent dialogue approval returned that receipt without another effect.
3. Native confirmation and adopted Loom dispatch were both observed waiting on the same PostgreSQL advisory-lock identity. After release, both returned the same receipt. There was one preparation, one backend operation and one suggestion for that intent.
4. Two completed-operation adoptions returned the original receipt without another approval, preparation, operation-ledger entry or suggestion. The original preparation's deadline was already past; completion did not renew it.
5. An adopted operation awaiting consent survived separate restarts of the owned bot, Loom API and sandbox/run worker. All preparation and operation documents, expiry and approval binding remained unchanged. Plain consent then committed the original backend ID once.
6. Plain rejection resolved the selected adopted action to Loom state `rejected`, with zero backend effects. Its independent native preparation remained unchanged. Actual delegated credentials also proved that another user and the wrong method receive 404, while unsupported modules/methods receive 422.

The final fixture contained eight completed Loom runs, fifteen completed bot tasks and twenty-two delivered outbox entries. Seven preparations produced six backend operations and six suggestions; the rejected action produced none. There were zero published articles.

## Contract and failure-path coverage

The local PostgreSQL knowledge suite passed 31 integration roots, including the inspector's native/agent and completed-receipt paths, expired-deadline preservation, foreign-owner/method denial, missing-contract rejection and current moderator revocation. Existing canonical confirmation tests cover changed terms, stale revisions and revoked authority. These use fixture identity peers; the separate runtime gates above use actual IAM.

Published Loom tests cover issuer/subject/actor/organization/service/method/revision mismatches, altered normalized terms, sealed backend identity, mandatory fresh consent, unsupported capability, expired preparations and unknown outcomes. Unknown operations remain status-only and keep the original backend ID. Those producer tests use real PostgreSQL and fixture HTTP peers; they are not relabelled as full consumer runtime tests. The source references are:

- [Loom operation inspection tests](https://github.com/complynx/loom/blob/34f4646832748efd455828f6804c6274027db659/internal/modules/operation_inspection_test.go)
- [Loom adoption coordinator tests](https://github.com/complynx/loom/blob/34f4646832748efd455828f6804c6274027db659/internal/operations/adoption_test.go)
- [Zinusya inspector tests](../integration/knowledge_inspection_test.go)
- [Zinusya native confirmation checks](../integration/knowledge_http_test.go)

Lost or unobserved responses were reconciled by reading the same original operation and receipt; the acceptance controller did not blindly repeat effects. Two earlier concurrency observations remain explicitly inconclusive: one counted only direct blockers, and one used an expired Mini App session. They are not accepted concurrency evidence. Further harness corrections fixed an invalid synthetic slug, a stored-field name and the expected rejection state. Failed observations and original IDs remain in the private evidence alongside the successful gates.

The inspector supports `suggest`, `edit`, `review` and `set_moderator`. Full actual-IAM handoff here exercises `suggest`; the other verbs have shared inspector/integration coverage. Other business modules receive explicit unsupported responses from the inspector. Their ordinary native and agent paths remain available.

## Evidence and cleanup

Independent review passes covered the source inspector and the acceptance controllers. Review findings about bounded lock cleanup, preservation of uncertain replies, sanitized identity evidence and actual suggestion-count assertions were fixed before the corresponding successful probes.

Private evidence is retained under `.cache/shuttle-work/knowledge-adoption-acceptance-20261021-v2/`, including original failed observations, model requests/responses, exact operation IDs, canonical receipts, restart snapshots and the mock Telegram journal under `.cache/shuttle-mock/kb-adopt-r2/`. These files contain fixture credentials and are ignored; they are not public reproduction artifacts.

All nine owned QA containers were stopped after terminal task/outbox checks. Four PostgreSQL archives were fully decoded with `pg_restore --file=/dev/null`; globals and final service logs were retained. Only then were this project's containers, volumes and network removed. No broad prune or production action was performed.
