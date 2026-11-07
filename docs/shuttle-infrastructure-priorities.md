# Product integration priorities

Keep Shuttle as one application with bounded domain modules. Loom owns the reactive agent loop; Shuttle owns durable business state, tokens, accepted work and delivery.

Every participant and operator workflow must share one business transaction across native commands/forms, agent tools and both continuation directions. A form save does not start an unsolicited agent run. The next explicit request can inspect the current state and continue the original operation.

Identity entry must preserve the authenticated Telegram sender, canonical IAM subject, namespace and current authority. Ordinary names, legal identity and Telegram metadata remain separate. Token handling stays server-side.

Registration, pair consent, pass assignment, payment evidence/review, service orders, meals, massage and avatar processing retain their own domain contracts. Menu visibility and discovery do not grant access. Uploaded evidence remains pending until an authorized human review records the decision.

Use one operation identity through preparation, explicit consent, execution and receipt recovery. Reopening a completed link reads the historical result; it does not create another effect. Unknown external outcomes require reconciliation rather than a replacement operation.

The [specification](shuttle-spec.md) defines behavior, the [QA matrix](shuttle-qa.md) defines proof boundaries, and the [roadmap](shuttle-roadmap.md) lists remaining work. Keep execution reports and installation details outside tracked product documentation.
