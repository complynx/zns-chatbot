# River startup and scheduling leases

Application readiness and available-job execution do not establish that scheduled or retryable job promotion has resumed. Inspect queue states together with collection health and freshness when diagnosing delayed work.

A successor scheduler can wait for the preceding leadership lease to expire. Preserve the lease and original evidence while investigating an interrupted startup or handover; do not delete or shorten it as a routine retry. One observed recovery is not a maximum latency guarantee.

Keep a metrics fixture independent of scheduler handover unless that handover is the behavior under test. Use one owned client for basic promotion/backlog/drain checks and a separate explicitly scoped scenario for competing-client recovery. See [queue metrics](shuttle-queue-metrics.md) and the [QA procedure](shuttle-qa.md).
