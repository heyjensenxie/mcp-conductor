<div align="center">

<img src="web/public/logo.png" alt="MCP Conductor" width="180" />

# MCP Conductor

**The control plane for your MCP ecosystem.**

_Route. Govern. Observe. Evaluate. Improve._

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/badge/Release-v1.0.0-blue)](CHANGELOG.md)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

</div>

<p align="center"><a href="README.md">简体中文</a> | <strong>English</strong></p>

MCP Conductor is an MCP gateway and control plane for aggregating, routing, governing, observing, testing, and evaluating MCP servers. It combines runtime governance with MCP quality engineering across the service lifecycle.

> **Current release: v1.0.0.** A single Go binary provides the gateway, control-plane API, and embedded web console.

## Overview

- **Aggregation:** expose tools from multiple upstream MCP servers through one `/mcp` endpoint, retaining upstream tool names such as `github.create_issue`. Cross-server name conflicts are previewed and skipped during discovery.
- **Routing:** resolve gateway tool names, apply route overrides, select healthy instances, and forward calls through HTTP or stdio.
- **Governance:** authenticate clients, authorize tools per access key, inject upstream credentials, enforce rate limits, and manage runtime IP policies.
- **Observability:** capture request and trace IDs, latency, status, server, tool, client, and instance dimensions without recording sensitive arguments by default.
- **Quality:** probe protocol behavior, calculate MCP quality scores, and execute regression suites against specific server instances.

## Features

| Capability | v1.0 status |
| --- | --- |
| Server registry, tool discovery, and collision-aware aggregation | Included |
| Stateless Streamable HTTP endpoint (`tools/list`, `tools/call`, `ping`, `initialize`) | Included |
| Multi-instance health checks and health-aware round-robin | Included |
| HTTP and local stdio upstream transports | Included |
| Tool metadata overrides and route management | Included |
| Console sessions, operator tokens, and data-plane access keys | Included |
| Per-key tool grants, call arguments, and request headers | Included |
| Memory/Redis sliding-window rate limits and runtime IP governance | Included |
| AES-256-GCM encrypted upstream credentials | Included with MySQL |
| Traffic logs, persisted minute trends, metrics, and replay | Included |
| MCP quality scoring and regression suites | Included |
| Vue 3 console embedded into the Go binary | Included |
| MySQL 5.7+ and in-memory storage drivers | Included |

Not included in v1.0: LLM judges, the planned Python evaluation worker, AI optimization, complex ABAC, Kafka, ClickHouse, Kubernetes operators, a service mesh, or a microservice split.

## Architecture

```text
MCP Client / AI Agent
          |
          | initialize · tools/list · tools/call
          v
+----------------------- MCP Conductor ------------------------+
| request ID -> auth -> authorization -> rate limit -> route   |
|            -> balance -> upstream -> metrics/audit           |
|                                                               |
| Control Plane: /api/*          Web Console: embedded Vue SPA  |
+-------------------------------+-------------------------------+
                                |
                                v
                      Upstream MCP Servers
```

MCP Conductor is a modular monolith: one Go backend owns the gateway and control plane, while the Vue console is built separately and embedded with `go:embed`. Core boundaries are kept behind storage, router, balancer, rate-limiter, and MCP-client interfaces.

See [Architecture Overview](docs/architecture/overview.md) and the [MySQL 5.7 compatibility contract](docs/architecture/database.md).

## Quick Start

### Requirements

- Go 1.25 or later
- Node.js 20 or later for building the console
- Docker and Docker Compose, if using the container workflow

### Build and run locally

```bash
make web-install
make build
./bin/mcp-conductor
```

Open `http://localhost:18110`. Authentication is enabled by default. If no administrator password is configured, the first startup prints a generated bootstrap password once.

For a persistent setup, configure secrets before starting:

```bash
export CONDUCTOR_AUTH_ADMIN_PASSWORD='<strong-password>'
export CONDUCTOR_AUTH_OPERATOR_TOKEN='<operator-token>'
export CONDUCTOR_AUTH_TOKEN_SECRET="$(openssl rand -hex 32)"
./bin/mcp-conductor
```

The `/mcp` data plane uses access keys created in the console or seeded through `CONDUCTOR_AUTH_API_KEYS`. Control-plane credentials and data-plane access keys are separate.

### Docker

The Compose file runs the application only. By default, it connects to an existing MySQL instance on the host through `host.docker.internal`; Redis remains disabled unless both Redis and rate limiting are enabled.

```bash
CONDUCTOR_DB_PASSWORD='<mysql-password>' docker compose up --build
```

Use `CONDUCTOR_DATABASE_DRIVER=memory` to run without MySQL. For MySQL, create the database and apply `database/schema.sql` first:

```bash
export CONDUCTOR_DATABASE_DSN='user:password@tcp(localhost:3306)/conductor?parseTime=true&loc=UTC&charset=utf8mb4'
make db-migrate
```

### Five-minute gateway example

Start the included mock server:

```bash
go run ./examples/mock-mcp
```

Register it through the control plane:

```bash
export OPERATOR='<CONDUCTOR_AUTH_OPERATOR_TOKEN>'

curl -s -X POST http://localhost:18110/api/servers \
  -H 'Content-Type: application/json' \
  -H "X-Api-Key: $OPERATOR" \
  -d '{"name":"Mock","endpoint":"http://localhost:9000/mcp","transport":"https"}'
```

List the discovered tools:

```bash
curl -s http://localhost:18110/api/tools -H "X-Api-Key: $OPERATOR"
```

Create a data-plane access key in the Console, then call a discovered tool:

```bash
export MCP_KEY='<data-plane-access-key>'

curl -s -X POST http://localhost:18110/mcp \
  -H 'Content-Type: application/json' \
  -H "X-Api-Key: $MCP_KEY" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"q":"policy"}}}'
```

## API Summary

- `POST /mcp`: MCP initialization, ping, aggregated tool listing, and routed tool calls.
- `/api/servers`: logical servers, instances, discovery, health tests, and upstream credentials.
- `/api/tools` and `/api/routes`: tool metadata, availability, and route overrides.
- `/api/keys`: access keys, tool grants, rotation, and identity-based test calls.
- `/api/metrics`, `/api/metrics/trend`, and `/api/logs`: metrics, persisted trends, traffic details, and replay.
- `/api/evaluations`: server quality probes and regression suites.
- `/api/runtime-config`: runtime governance and observability settings.
- `/healthz` and `/readyz`: liveness and readiness probes.

Control-plane responses use the envelope `{ "code", "message", "request_id", "data" }`. Internal SQL errors and stack traces are not returned to clients.

## Configuration

Configuration is loaded from `config.yaml`; `CONDUCTOR_*` environment variables take precedence. Start from [config.example.yaml](config.example.yaml) or [.env.example](.env.example).

| Group | Purpose |
| --- | --- |
| `server` | Bind address, port, trusted proxies |
| `database` | `memory` or `mysql` storage and DSN |
| `redis` | Redis connection used by distributed rate limiting |
| `gateway` | Upstream timeout and concurrency |
| `ratelimit` | Key, IP, and global sliding-window limits |
| `security` | Seed IP blocklist and whitelist |
| `auth` | Console administrator, sessions, operator token, bootstrap keys |
| `credentials` | AES-256 encryption key for stored upstream secrets |
| `logging` | Level and text/JSON output |
| `observability` | Sampling, argument capture, trend and traffic retention |

Production SQL and migrations intentionally target MySQL 5.7+. MySQL 8-only CTEs, window functions, functional indexes, and JSON-indexing features must not be introduced.

## Development

```bash
make web-dev       # Vue development server on :5173
make build         # Console + embedded Go binary
make build-backend # Backend using the currently embedded console
make test          # Go tests
make vet           # Go static analysis
make check         # vet + tests + frontend build/type check
```

Real MySQL integration tests require `MYSQL_TEST_DSN` and `database/schema.sql`. On supported environments, `go test -race ./...` provides additional concurrency coverage.

## Roadmap

- **v1.0:** stable gateway and control-plane foundation, console, governance, observability, basic evaluation, MySQL persistence, and Docker delivery.
- **v1.1 candidates:** richer route management, persistent stdio sessions, release automation, and more deployment examples.
- **Later:** persisted evaluation datasets and test cases, comparison workflows, LLM judges, broader protocol compatibility, and performance testing.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Report vulnerabilities through the private channels described in [SECURITY.md](SECURITY.md), not through public issues.

## License

[Apache License 2.0](LICENSE) © MCP Conductor contributors.
