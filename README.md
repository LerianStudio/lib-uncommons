# lib-uncommons

`lib-uncommons` is Lerian's shared Go toolkit for service primitives, connectors, observability, and runtime safety.

The current major API surface is **v2**. If you are migrating from older code, see `MIGRATION_MAP.md`.

## Requirements

- Go `1.25.7` or newer

## Installation

```bash
go get github.com/LerianStudio/lib-uncommons/v2
```

## What is in this library

### Core (`uncommons`)

- `app.go`: launcher and app lifecycle helpers
- `context.go`: safe timeout helpers and request-scoped logger/tracer/metrics/header-id tracking (deprecated context helpers `NewTracerFromContext`, `NewMetricFactoryFromContext`, `NewHeaderIDFromContext`, and `WithTimeout` are now removed)
- `errors.go`: standardized business error mapping
- `utils.go`, `stringUtils.go`, `time.go`, `os.go`: utility set used across Lerian services
- `uncommons/constants`: shared constants for datasources, errors, headers, metadata, pagination, transactions, etc.

### Observability and logging

- `uncommons/opentelemetry`: telemetry bootstrap (`NewTelemetry`), propagation, span helpers, redaction
- `uncommons/opentelemetry/metrics`: fluent metrics factory with explicit error returns
- `uncommons/log`: v2 logging interface (`Logger` with `Log`/`With`/`WithGroup`/`Enabled`/`Sync`), typed `Field` constructors, log-injection prevention
- `uncommons/zap`: zap adapter for `uncommons/log` with OTEL bridge support and explicit `Config`-based construction with `New()`

### Data and messaging connectors

- `uncommons/postgres`: explicit `Config` constructor + `Migrator` with thread-safe connection manager
- `uncommons/mongo`: `Config`-based client with functional options, URI builder, and index helpers
- `uncommons/redis`: topology-based `Config` (standalone/sentinel/cluster) + IAM auth and distributed locking (Redsync); TLS defaults to a TLS1.2 minimum floor, with `AllowLegacyMinVersion` as an explicit temporary compatibility override
- `uncommons/rabbitmq`: connection/channel/health helpers, confirmable publisher with broker acks and auto-recovery, and DLQ topology utilities (`AllowInsecureHealthCheck`, `HealthCheckAllowedHosts`, `RequireHealthCheckAllowedHosts` for health-check rollout hardening; basic-auth health checks require allowlist unless insecure compatibility is explicitly enabled)

### HTTP and server utilities

- `uncommons/net/http`: Fiber HTTP helpers (response/error/context parsing/SSRF-protected reverse proxy/middleware)
- `uncommons/net/http/ratelimit`: Redis-backed rate limit storage utilities
- `uncommons/server`: `ServerManager`-based graceful shutdown and lifecycle helpers

### Resilience and safety

- `uncommons/circuitbreaker`: service-level circuit breaker manager and health checker with error-returning constructors
- `uncommons/backoff`: exponential backoff with jitter and context-aware sleep
- `uncommons/errgroup`: error-group concurrency helpers with panic recovery
- `uncommons/runtime`: panic recovery, panic metrics, safe goroutine wrappers
- `uncommons/assert`: production-safe assertion primitives with telemetry integration
- `uncommons/safe`: panic-safe wrappers (math/regex/slice operations)
- `uncommons/security`: sensitive field handling and obfuscation helpers

### Domain and support packages

- `uncommons/transaction`: intent-based transaction planning, balance eligibility validation, and posting flow
- `uncommons/outbox`: transactional outbox contracts, dispatcher, sanitizer, and PostgreSQL adapters for schema-per-tenant or column-per-tenant models (schema resolver requires tenant context by default; column migration uses composite key `(tenant_id, id)`)
- `uncommons/crypto`: hashing and symmetric encryption helpers
- `uncommons/jwt`: HS256/384/512 JWT signing and verification
- `uncommons/license`: license validation and enforcement helpers
- `uncommons/pointers`: pointer helper utilities
- `uncommons/cron`: cron expression parser and scheduler

## Minimal v2 usage

```go
import (
    "context"

    "github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
    "github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry"
)

func bootstrap() error {
    logger := log.NewNop()

    tl, err := opentelemetry.NewTelemetry(opentelemetry.TelemetryConfig{
        LibraryName:               "my-service",
        ServiceName:               "my-service-api",
        ServiceVersion:            "2.0.0",
        DeploymentEnv:             "local",
        CollectorExporterEndpoint: "localhost:4317",
        EnableTelemetry:           false, // Set to true when collector is available
        InsecureExporter:          true,
        Logger:                    logger,
    })
    if err != nil {
        return err
    }
    defer tl.ShutdownTelemetry()

    tl.ApplyGlobals()

    _ = context.Background()

    return nil
}
```

## Development commands

- `make test` - run all tests
- `make test-unit` - run unit tests
- `make test-integration` - run integration tests with testcontainers
- `make test-all` - run all tests (unit + integration)
- `make coverage-unit` - run unit tests with coverage report
- `make coverage-integration` - run integration tests with coverage report
- `make coverage` - run all coverage targets
- `make lint` - run lint checks
- `make lint-fix` - auto-fix lint issues
- `make format` - format code
- `make build` - build all packages
- `make clean` - clean build artifacts
- `make tidy` - clean dependencies
- `make sec` - run security checks using gosec
- `make tools` - install test tools (gotestsum)

## License

This project is licensed under the terms in `LICENSE`.
