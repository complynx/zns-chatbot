# Bounded PostgreSQL integration tests

`tools/test-integration.py` runs the complete integration package in sequential
processes of at most 100 root tests. Each process uses the race detector,
`-count=1`, at most two parallel tests and a 10-minute timeout. Nested subtests
remain selected. Discovery has a five-minute wall-clock limit; each batch has
a twelve-minute outer limit that includes compilation and dependency resolution.
The outer limit terminates the owned process tree, retains partial stdout/stderr,
and records `timed_out` with exit code 124. Cleanup failure rejects the runner.
These limits do not alter application timeouts, delivery uncertainty rules or
business assertions.

The runner requires Python 3 from the standard library, Go with CGO and a C
compiler, and a disposable migrated-by-tests PostgreSQL database. These are test
dependencies. The production bot and its container do not require Python or GCC.
Fixtures create separate schemas. Use only a database owned by the test runner.

Set `SHUTTLE_TEST_DATABASE_URL` to that database, then run from the checkout:

```sh
python3 -m unittest discover -s tools -p test_integration_runner.py
python3 tools/test-integration.py /tmp/zinusya-integration-new-run
```

The output directory must be new. Discovery output, raw JSON test events,
stderr and `summary.json` remain there. Failed batches also print their Go
output for CI diagnosis. A later batch still runs after an earlier failure;
there is no automatic retry. Process interruption leaves the aggregate result
unsuccessful. Archive useful evidence before removing the disposable database.

Success requires every discovered root exactly once, a successful Go package
result and no skipped or failed tests, including nested subtests. Missing,
duplicate or unexpected roots, malformed output and nonzero process exit reject
acceptance. An absent database configuration rejects the command before output
creation. The result covers `./integration`; CI runs `./cmd/...`, `./internal/...`
and `./migrations/...` separately with the race detector. The CI job keeps its
60-minute overall deadline; individual batch limits do not extend it.
