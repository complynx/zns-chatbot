# Developer validation: functional provider and stand templates

Base: 5d81be9a4fa7769f3b7addf6847c6316278833b2.
Input: preserved R104 provider reassessment rev3. Its original main.go,
server.go and fake.go guards matched before import. The candidate has been
adapted for pinned lint; fresh review must cover the full Git diff.

Owned source: cmd/zns/main.go and fake_server.go; internal/sandbox/fake.go,
edit_delay.go, edit_delay_journal.go and three edit_delay*_internal_test.go files.
Stand preparation owns docs/sandbox/fqa-stands only.

Checks completed after cleanup:

- go test ./internal/sandbox ./cmd/zns -count=1: exit 0, both packages passed.
- Pinned golangci-lint fmt --diff ./cmd/zns ./internal/sandbox: exit 0, no diff.
- Pinned golangci-lint run ./cmd/zns ./internal/sandbox: exit 0, zero issues.
- Native Windows race command: unavailable because cgo disabled/no local C
  compiler. This was not counted as passed.
- Actual existing zns-go-race-tools:local image, --network none, read-only module
  cache and isolated task build cache: go test -race ./internal/sandbox
  -run TestEditDelay -count=1: exit 0, passed.
- All nine stand Compose files: config --no-interpolate --no-consistency --quiet
  exit 0. This proves parsing only, not actual image binding or deployment.
- git diff --check: exit 0.

No suppressions, gate changes, dependency downloads, image pulls, shared database
writes, stand startup or root merge. Current application/helper images still
require build/provenance from the final reviewed merged Git epoch. No independent
Code/Functional QA or full stage acceptance is claimed by developer checks.
