# Telemetry dependency review

Reviewed 2026-09-26. Use `go.opentelemetry.io/otel` and trace SDK/OTLP HTTP
exporter v1.46.0, plus `github.com/prometheus/client_golang` v1.24.1.
Both projects use Apache 2.0. Exact versions are also direct dependencies in
[Grafana Alloy's manifest](https://github.com/grafana/alloy/blob/main/go.mod).
This is identifiable downstream use; no star or download count was used as
evidence. It does not certify the absence of artificial popularity signals.

The Go module proxy reports stable release dates 2026-08-25 for OTel v1.46.0
and 2026-07-24 for client_golang v1.24.1. OTel's newer v1.47 release candidate
is not selected. Official [Go documentation](https://opentelemetry.io/docs/languages/go/)
and active release streams provide maintenance evidence.

Security checks include upstream [OTel policy/advisories](https://github.com/open-telemetry/opentelemetry-go/security)
and [Prometheus policy/advisories](https://github.com/prometheus/client_golang/security).
OTel [GHSA-mh2q-q3fh-2475](https://github.com/open-telemetry/opentelemetry-go/security/advisories/GHSA-mh2q-q3fh-2475)
affects multi-header baggage extraction through v1.40.0 and is patched in v1.41.0.
The selected version is newer; this application should propagate trace context
only, without user-supplied baggage. Prometheus
[GHSA-cg3q-j54f-5p7p](https://github.com/prometheus/client_golang/security/advisories/GHSA-cg3q-j54f-5p7p)
documents unbounded method-label cardinality. Instrumentation must use bounded
method/operation/route labels, never raw paths, IDs, query strings or user text.

Selection is not vulnerability-scan acceptance. Run pinned `govulncheck` against
the linked implementation and review its transitive graph before accepting this
stage. Keep Dependabot and the existing quality gates enabled.

## Linked package verification

`internal/observability` also directly uses `github.com/felixge/httpsnoop v1.1.0`
to preserve HTTP ResponseWriter optional interfaces. Its
[tagged license](https://github.com/felixge/httpsnoop/blob/v1.1.0/LICENSE.txt) is
MIT and its module has no third-party requirements. The local Go module proxy
metadata identifies release time 2026-06-11 and commit
`0fc9006be0bfd68ee14bc3db0d58f7c7241892e0`. Actual downstream adoption is visible
in [OpenTelemetry's HTTP handler implementation](https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/instrumentation/net/http/otelhttp/handler.go),
which imports and calls `httpsnoop.Wrap`. This is a small protocol utility;
its release date and current downstream use support selection without claiming
a high release cadence. The GitHub advisory page could not be retrieved; the
successful Go vulnerability scan below is the verified security evidence.

Local module-cache license files were inspected for all 32 external modules
linked by this package. They use Apache-2.0, MIT, or BSD-style licenses; no
missing or unrecognized top-level license file was found. This includes the
selected OTel modules, Prometheus/client_model/common, httpsnoop, pgx and its
helpers, grpc-gateway/gRPC/protobuf/genproto, Go x modules, go-logr, uuid,
backoff, xxhash, perks, and goautoneg. The runtime adds no unreviewed alternative
telemetry adapter. Preserve upstream notices when distributing dependencies.

The first symbol scan found no reachable vulnerability but reported the
unreachable gRPC server advisory
[GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) in v1.83.1. The selected
graph was updated to `google.golang.org/grpc v1.83.2`. The final command on
2026-09-26 scanned 33 modules including the application and Go 1.27.0:

```text
tools.local/govulncheck.exe -show verbose ./internal/observability/...
No vulnerabilities found.
```

Focused package tests and vet passed, and pinned golangci-lint reported zero
issues. OTLP delivery is verified against a real local HTTP collector stub;
no remote Grafana credentials or deployed collector were used. Full runtime
integration and independent QA remain separate gates.
