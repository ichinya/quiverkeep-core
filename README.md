# quiverkeep-core

> Core service and CLI for QuiverKeep.

`quiverkeep-core` is the system of record for the product. This repository owns domain logic, configuration, storage,
API contracts, and the Go CLI entrypoint.

## Responsibilities

- run the core server;
- expose versioned contracts for clients;
- own usage, limits, reset, and provider normalization logic;
- manage storage and migrations;
- provide CLI commands without duplicating business rules.

## Boundaries

- desktop and web clients consume contracts from core;
- no UI repository may read storage directly;
- business logic must not be duplicated outside core.

## Status

Core Base implementation is present and runnable.

## Verified Snapshot (2026-03-15)

- Go module and single binary entrypoint (`cmd/quiverkeep`).
- Config loader with precedence `flags > env > file > defaults`.
- Canonical config path via OS config dir with legacy fallback to `~/.quiverkeep/config.json`.
- SQLite storage (`modernc.org/sqlite`) with migration bootstrap and lock file.
- HTTP API endpoints:
    - `GET /api/v1/status`
    - `GET /api/v1/usage`
    - `GET /api/v1/limits`
    - `GET /api/v1/subscriptions`
    - `GET /api/v1/providers`
    - `POST /api/v1/proxy/anthropic/messages`
    - `GET /api/v1/proxy/status`
- Thin CLI commands:
    - `serve`, `status`, `usage`, `limits`, `proxy status`, `config show`, `config path`, `doctor`, `version`
- Structured logging with configurable `LOG_LEVEL` and JSON output.
- Contract/integration/perf/security tests (`go test ./...`).

## Quick Start

```powershell
Set-Location D:\Projects\aifhub\quiverkeep\quiverkeep-core
go test ./...
go run .\cmd\quiverkeep serve
```

In another terminal:

```powershell
Set-Location D:\Projects\aifhub\quiverkeep\quiverkeep-core
go run .\cmd\quiverkeep status
go run .\cmd\quiverkeep usage --json
go run .\cmd\quiverkeep proxy status --json
go run .\cmd\quiverkeep doctor --json
```

## Proxy Mode (v0.2)

- `POST /api/v1/proxy/anthropic/messages` forwards Anthropic Messages API payloads through core.
- Upstream non-success and timeout outcomes are mapped to stable core errors (`PROXY_UPSTREAM_ERROR`, `PROXY_TIMEOUT`).
- Proxy diagnostics are exposed through `GET /api/v1/proxy/status`.
- Provider-spending proxy calls require core bearer auth even in loopback mode.

## Logging Policy

- Structured logs only (JSON).
- Required keys in operational flows: `component`, `operation`, `request_id`, `duration_ms`, `error_code` when
  applicable.
- Proxy operations must include: `provider`, `operation=proxy_forward|proxy_usage|proxy_status`, `retry_decision`,
  and upstream `status` where applicable.
- Levels:
    - `DEBUG` for detailed flow and override resolution.
    - `INFO` for lifecycle and successful operations.
    - `WARN` for recoverable issues (auth mismatch, fallback, stale lock).
    - `ERROR` for failed operations and exits.
- Control:
    - `QUIVERKEEP_LOG_LEVEL` (or `--log-level`) controls verbosity.
    - Optional file sink via config `logging.path`.

## Troubleshooting

- `PORT_IN_USE`: run `quiverkeep serve --port 9000`.
- `STORAGE_LOCK_ERROR`: check whether another core instance is active; stale lock is auto-cleaned.
- `UNAUTHORIZED`: verify `QUIVERKEEP_TOKEN` / `Authorization: Bearer ...` policy for remote mode.
- `PROXY_DISABLED` / `PROXY_NOT_CONFIGURED`: enable proxy mode and set `ANTHROPIC_API_KEY`.
- `PROXY_UPSTREAM_ERROR` / `PROXY_TIMEOUT`: inspect proxy logs and `/api/v1/proxy/status` diagnostics.

## Quality Gates

Required completion gates for this repository:

```powershell
go test ./...
go build ./cmd/quiverkeep
go vet ./...
```

Lint target is not defined yet in this repository (`Makefile` / `Taskfile` is absent), so lint checks are currently
documented as pending automation.

## Repository Boundary Runbook

`quiverkeep-core` is an independent repository. Implementation and commits for core tasks must be done in this
repository branch, not in workspace root.

1. Open `quiverkeep-core`
2. Verify branch: `codex/feat/core-base-runtime`
3. Run build/test commands from this repository only
4. Commit in `quiverkeep-core` repository lifecycle
