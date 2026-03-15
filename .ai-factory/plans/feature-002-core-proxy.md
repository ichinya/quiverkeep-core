# Implementation Plan: 002-core-proxy (v0.2)

Branch: feature/002-core-proxy
Created: 2026-03-15

## Settings
- Testing: yes
- Logging: verbose
- Docs: yes

## Roadmap Linkage
Milestone: "002-core-proxy (v0.2)"
Rationale: Delivers the first provider-aware API proxy slice required by `core#2` before generalized proxy work in `core#8`.

## Research Context
Source: workspace `.ai-factory/RESEARCH.md` (Active Summary)

Goal:
- Consolidate artifacts and GitHub scope for `v0.2 / 002-core-proxy` and convert them into an executable plan.

Constraints:
- Keep transport proxy scope separate from passive collectors.
- Preserve auth boundary: `client -> core` token is separate from `core -> provider` keys.
- Implement only first provider-aware `API proxy (L7)` slice; defer `HTTP proxy / CONNECT` to `2.1`.

Decisions:
- Source of truth: roadmap and architecture docs plus `core#2` comment clarifying scope boundaries.
- `core#2` is the implementation anchor for this plan.
- `core#3` and `core#8` remain adjacent but out-of-scope for this delivery slice.

Open questions:
- Minimal diagnostics contract for proxy mode in API and CLI.
- Required Anthropic/Claude fields for first accounting extraction.

## Commit Plan
- **Commit 1** (after tasks 1-3): `feat(proxy): add v0.2 proxy contracts, config, and HTTP endpoints`
- **Commit 2** (after tasks 4-6): `feat(proxy): add anthropic forwarding, accounting, and proxy status diagnostics`
- **Commit 3** (after tasks 7-8): `feat(cli): expose proxy diagnostics and doctor integration`
- **Commit 4** (after tasks 9-10): `test+docs(proxy): expand proxy coverage and finalize release-quality checks`

## Tasks

### Phase 1: Contracts and scaffolding
- [x] Task 1: Define v0.2 proxy contract surface and config shape.
  Files: `api/openapi/v1/openapi.yaml`, `config/config.example.json`, `internal/config/config.go`, `internal/config/config_test.go`.
  Deliverables: add `API proxy (L7)` endpoint contracts for Anthropic forwarding and proxy diagnostics (`/api/v1/proxy/status`), and define proxy config fields (enabled flag, upstream base URL override, timeout).
  Logging requirements: log proxy config resolution at `DEBUG` (`component=config`, fields for enabled mode, base URL, timeout), and log invalid proxy config values at `WARN` with stable error code mapping.

- [x] Task 2: Create proxy module skeleton for provider-aware API proxy (`internal/proxy/api`) and diagnostics state.
  Files: `internal/proxy/api/*.go` (new), optional shared types in `internal/domain` if needed.
  Deliverables: service interface for forward/status flows, upstream client abstraction, and in-memory diagnostics model for last successful/failed proxy attempts.
  Logging requirements: log request lifecycle checkpoints at `DEBUG` (`request_id`, provider, model hint, endpoint), upstream call start/finish at `INFO`, and upstream/network failures at `ERROR` with provider metadata.
  Depends on: Task 1.

- [x] Task 3: Add auth-gated proxy HTTP endpoint registration and input validation in core API layer.
  Files: `internal/api/handlers/handlers.go`, `internal/api/server/server.go`, `internal/api/server/server_test.go`.
  Deliverables: register forward and status routes, enforce method/content validation, and keep handlers thin by delegating provider-specific logic to proxy module.
  Logging requirements: log proxy endpoint invocation at `INFO`, method/path validation failures at `WARN`, and policy/auth rejections at `WARN` with `request_id` and route.
  Depends on: Task 1, Task 2.

### Phase 2: Core proxy behavior and CLI surface
- [x] Task 4: Implement Anthropic-first upstream forwarding and response passthrough for the v0.2 API proxy path.
  Files: `internal/proxy/api/*.go`, `internal/api/handlers/handlers.go`, `internal/errors/codes.go` (if new proxy-specific codes are required).
  Deliverables: build upstream request with server-side Anthropic key, forward payload/headers safely, and map upstream failures to stable core error envelopes without leaking secrets.
  Logging requirements: log upstream latency, status code, retry/no-retry decision, and normalized failure envelope; include `provider=anthropic` and `operation=proxy_forward` in all proxy flow logs.
  Depends on: Task 2, Task 3.

- [x] Task 5: Extract usage/accounting metadata from proxied responses and persist via shared usage pipeline.
  Files: `internal/proxy/api/*.go`, `internal/storage/store.go` (only if persistence helpers need extension), `internal/domain/models.go` (only if metadata model requires extension).
  Deliverables: parse provider usage fields from proxy response path, normalize to current usage model, and degrade gracefully if extraction fails while keeping proxy response path functional.
  Logging requirements: log extracted accounting fields at `DEBUG` (tokens/cost/error class), persistence success at `INFO`, and partial extraction/persistence degradation at `WARN` without corrupting totals.
  Depends on: Task 4.

- [x] Task 6: Implement proxy diagnostics/status API behavior and expose transport-safe metadata.
  Files: `internal/proxy/api/*.go`, `internal/api/handlers/handlers.go`, `internal/api/handlers/handlers_test.go`.
  Deliverables: return proxy enabled/configured state, last upstream status, last error code/message, and timestamps without exposing provider secrets or payload content.
  Logging requirements: log diagnostics reads at `DEBUG`, state transitions at `INFO`, and stale/invalid status state at `WARN`.
  Depends on: Task 2, Task 4, Task 5.

- [x] Task 7: Extend CLI with proxy diagnostics and health visibility for v0.2.
  Files: `internal/cli/commands.go`, `internal/cli/httpclient/client.go`, `internal/cli/doctor/doctor.go`, `internal/cli/cli_test.go`, `internal/cli/doctor/doctor_test.go`.
  Deliverables: add proxy diagnostics command surface and include proxy checks in doctor output using the new status endpoint contract.
  Logging requirements: log proxy check start/finish and duration at `INFO`, response parsing details at `DEBUG`, and user-facing diagnostic failures at `ERROR` with mapped stable error codes.
  Depends on: Task 6.

- [x] Task 8: Harden proxy flow for security and observability boundaries.
  Files: `internal/proxy/api/*.go`, `internal/api/middleware/logging.go` (only if log field extensions are needed), `test/security/security_test.go`.
  Deliverables: ensure proxy logs never include provider keys or raw sensitive payload fragments, and add security assertions for redaction and auth-gated provider-spending paths.
  Logging requirements: log redaction decisions at `DEBUG` (without secret values), auth boundary violations at `WARN`, and security-relevant execution failures at `ERROR`.
  Depends on: Task 4, Task 5, Task 6.

### Phase 3: Verification and documentation
- [x] Task 9: Add comprehensive test coverage for proxy endpoint, status diagnostics, auth behavior, and accounting persistence.
  Files: `internal/api/handlers/handlers_test.go`, `internal/api/server/server_test.go`, `test/integration/*proxy*_test.go` (new), `test/perf/*proxy*_test.go` (new if needed), plus proxy module tests under `internal/proxy/api`.
  Deliverables: cover success/error/timeout paths, remote-mode auth enforcement, local-loopback behavior, diagnostics endpoint behavior, and proxy latency budget checks.
  Logging requirements: test fixtures should assert stable structured fields in critical logs where behavior relies on auditability (auth rejection, upstream error, accounting write path).
  Depends on: Task 7, Task 8.

- [x] Task 10: Update contracts/docs and run release-quality gates for v0.2 proxy slice.
  Files: `api/openapi/v1/openapi.yaml`, `README.md`, any proxy notes required under `.ai-factory` in this repo.
  Deliverables: synchronize API docs with implemented behavior, document proxy diagnostics and auth expectations, and run `go test ./...`, `go build ./cmd/quiverkeep`, `go vet ./...`, and repository lint checks (or document if lint target is not yet defined) as mandatory completion gates.
  Logging requirements: document expected log keys and levels for proxy operations, including minimum observability fields required in production.
  Depends on: Task 9.
