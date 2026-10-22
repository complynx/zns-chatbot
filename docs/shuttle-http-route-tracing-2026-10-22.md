# Private HTTP route tracing, 2026-10-22

HTTP server spans now include the matched ServeMux route template as
`http.route`. Method prefixes are removed, while parameters remain placeholders:
`GET /users/{id}` becomes `/users/{id}`, never the actual user path. This is a
span attribute only; metric labels remain unchanged.

## Mechanism and privacy

The outer telemetry middleware retains the request passed to the handler so it
can read `Request.Pattern` after routing. The bounded-read API wrapper also
preserves that pattern across its deadline-context clone. Deadline and socket
behavior remain unchanged.

404 and405 responses without a matched route omit the attribute. CONNECT omits
it even when a handler matches: Go1.27's trailing-slash redirect can put a raw
request path in `Pattern`. The privacy test reproduced `/trees/private-user/`
in the attribute before that exclusion. Ordinary redirects still use templates.
Headers, query values and request identifiers are not added to traces.

## Verification

The new behavioral test first failed on missing GET, HEAD and methodless-route
templates. Independent review then found the CONNECT privacy edge. The first
redirect expectation used301; the actual target Go1.27 behavior is307. After
correcting that expectation, the test reproduced the raw-path disclosure.
CONNECT exclusion fixed it.

The closest observability and API packages pass on Windows and on Linux with
`-race -p 1 -parallel 2 -count=1`. Coverage includes templates, HEAD fallback,
methodless handlers, redirects, unmatched routes,405, private request values,
and pattern propagation through bounded API reads. The targeted golangci-lint
run reports zero issues. The initial sandboxed Windows command could not access
the Go toolchain/cache; its authorized rerun reached the intended failing test.

Two independent source-review passes closed the CONNECT finding; the final pass
had no actionable findings. The Linux QA container exited0 without OOM and was
removed after its result/state were saved. Evidence remains under
`.cache/shuttle-work/container-results/zinusya-http-route-qa-20261022.*` and
`.cache/shuttle-work/http-route-tracing-20261022-v1/`.

## Runtime boundary

This is source/package acceptance. A new candidate image must be built and
installed on the owned fixture before claiming installed `http.route` behavior.
The prepared approval trace controller has not run. Its stronger check requires
the exact knowledge-confirm route, HTTP200 POST, the backend commit interval,
and exported causal ancestry to the consumed-consent execution step in the SAME
HTTP span. Approval continuation and primary-backend visibility remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
