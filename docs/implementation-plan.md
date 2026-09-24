# Cosmoseed hardening plan

## Root Cause / Goal

Cosmoseed currently treats address-book membership or historical CometBFT
"good" status as evidence of reachability. Configuration merging also loses
explicit values, and startup, observability, release validation, and Kubernetes
packaging do not yet provide a dependable operator contract.

Deliver one cohesive pull request that makes both PEX and HTTP serve only
recently authenticated outbound peers, fixes configuration and lifecycle
behavior, adds focused regression tests and operational endpoints, validates
the release path, and provides a persistent StatefulSet Helm chart.

## Approach

1. Load configuration as defaults, then YAML, explicitly present environment
   variables, and explicitly supplied flags. Validate before side effects and
   atomically persist the effective configuration when allowed.
2. Own bounded peer-verification metadata in the seed reactor. Record evidence
   only from authenticated outbound `AddPeer` callbacks and expire it by TTL.
3. Wrap the raw CometBFT address book so PEX and HTTP select the same fresh
   verified peers, while raw entries remain candidates for bounded rechecks.
   Preserve upstream PEX request throttling and unsolicited-response handling.
4. Make run and stop context-driven, synchronized, idempotent, and complete.
   Bind listeners synchronously, configure HTTP timeouts, and propagate errors.
5. Preserve default CSV output while adding validated limits, JSON metadata,
   TOML output, health, readiness, status, and isolated Prometheus metrics.
6. Add test-first coverage for configuration, verification, PEX integration,
   lifecycle, HTTP, metrics, chart rendering, and compatibility.
7. Repair local tooling and CI/release validation, then package a secure,
   persistent StatefulSet Helm chart and document the v0.12 migration.

## Critical Files

- `Makefile` and `.github/workflows/*` - safe and reproducible checks/releases.
- `cmd/cosmoseed/*` - explicit CLI/env precedence and signal context.
- `pkg/cosmoseed/*` - configuration, listeners, HTTP, metrics, and lifecycle.
- `pkg/seedreactor/*` - verification store, scheduling, and address-book wrapper.
- `charts/cosmoseed/**` - persistent Kubernetes deployment contract.
- `README.md`, `config.example.yaml`, `docs/migration-v0.12.md` - operator docs.

## Risks & Side Effects

- Cold starts intentionally serve no peers until outbound authentication succeeds.
- Strict YAML rejects obsolete or misspelled fields instead of ignoring them.
- Effective flag/environment values are persisted unless read-only mode is set.
- Verification expiry must remain independent of scheduler load.
- Address-book callbacks and verification locks must not introduce lock inversion.
- Chart rendering proves intent, not a live deployment.

## Scope

Files: approximately 35-45 | Lines: approximately 2,000-3,000 | Complexity: high

One pull request with reviewable commits. No tag, publication, deployment, or
merge. Latency/ASN/subnet ranking and persisted verification evidence are
deferred until the project has reliable measurements and an agreed policy.

## Verification

- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go vet ./...`
- `make build lint`
- `git diff --check`
- `goreleaser check`
- `goreleaser release --snapshot --clean --skip=publish`
- `helm lint charts/cosmoseed --set config.chainID=test-chain`
- `helm template test charts/cosmoseed --set config.chainID=test-chain`
