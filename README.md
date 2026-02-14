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
- `uncommons/redis`: topology-based `Config` (standalone/sentinel/cluster) + IAM auth and distributed locking (Redsync)
- `uncommons/rabbitmq`: connection/channel/health helpers for AMQP with context-aware methods

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

## Environment Variables

The following environment variables are recognized by lib-uncommons:

| Variable | Type | Default | Package | Description |
| :--- | :--- | :--- | :--- | :--- |
| `VERSION` | `string` | `"NO-VERSION"` | `uncommons` | Application version, printed at startup by `InitLocalEnvConfig` |
| `ENV_NAME` | `string` | `"local"` | `uncommons` | Environment name; when `"local"`, a `.env` file is loaded automatically |
| `ENV` | `string` | _(none)_ | `uncommons/assert` | When set to `"production"`, stack traces are omitted from assertion failures |
| `GO_ENV` | `string` | _(none)_ | `uncommons/assert` | Fallback production check (same behavior as `ENV`) |
| `LOG_LEVEL` | `string` | `"debug"` (dev/local) / `"info"` (other) | `uncommons/zap` | Log level override (`debug`, `info`, `warn`, `error`); `Config.Level` takes precedence if set |
| `LOG_ENCODING` | `string` | `"console"` (dev/local) / `"json"` (other) | `uncommons/zap` | Log output format: `"json"` for structured JSON, `"console"` for human-readable colored output |
| `LOG_OBFUSCATION_DISABLED` | `bool` | `false` | `uncommons/net/http` | Set to `"true"` to disable sensitive-field obfuscation in HTTP access logs (**not recommended in production**) |
| `METRICS_COLLECTION_INTERVAL` | `duration` | `"5s"` | `uncommons/net/http` | Background system-metrics collection interval (Go duration format, e.g. `"10s"`, `"1m"`) |
| `ACCESS_CONTROL_ALLOW_CREDENTIALS` | `bool` | `"false"` | `uncommons/net/http` | CORS `Access-Control-Allow-Credentials` header value |
| `ACCESS_CONTROL_ALLOW_ORIGIN` | `string` | `"*"` | `uncommons/net/http` | CORS `Access-Control-Allow-Origin` header value |
| `ACCESS_CONTROL_ALLOW_METHODS` | `string` | `"POST, GET, OPTIONS, PUT, DELETE, PATCH"` | `uncommons/net/http` | CORS `Access-Control-Allow-Methods` header value |
| `ACCESS_CONTROL_ALLOW_HEADERS` | `string` | `"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization"` | `uncommons/net/http` | CORS `Access-Control-Allow-Headers` header value |
| `ACCESS_CONTROL_EXPOSE_HEADERS` | `string` | `""` | `uncommons/net/http` | CORS `Access-Control-Expose-Headers` header value |

Additionally, `uncommons.SetConfigFromEnvVars` populates any struct using `env:"VAR_NAME"` field tags, supporting `string`, `bool`, and integer types. Consuming applications define their own variable names through these tags.

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

## Project Rules

For coding standards, architecture patterns, testing requirements, and development guidelines, see [`docs/PROJECT_RULES.md`](docs/PROJECT_RULES.md).

## License

This project is licensed under the terms in `LICENSE`.
