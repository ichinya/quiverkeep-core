# quiverkeep-core

> Core service and CLI for QuiverKeep.

`quiverkeep-core` is the system of record for the product. This repository will own domain logic, configuration, storage, API contracts, proxy flows, collectors, and the Go CLI entrypoint.

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

Repository initialized. Implementation has not started yet.
