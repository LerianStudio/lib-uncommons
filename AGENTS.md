# AGENTS

This file provides repository-specific guidance for coding agents working on `lib-uncommons`.

## Project snapshot

- Module: `github.com/LerianStudio/lib-uncommons`
- Language: Go
- Go version: `1.25.7` (see `go.mod`)
- Current API generation: v2 (breaking changes already applied in multiple packages)

## Primary objective for changes

- Preserve v2 public API contracts unless a task explicitly asks for breaking changes.
- Prefer explicit error returns over panic paths in production code.
- Keep behavior nil-safe and concurrency-safe by default.

## Repository shape

Root:
- `uncommons/`: root shared helpers (`app`, `context`, `errors`, utilities)

Observability and logging:
- `uncommons/opentelemetry`: telemetry bootstrap, propagation, redaction
- `uncommons/opentelemetry/metrics`: metric factory + fluent builders
- `uncommons/log`, `uncommons/zap`: logging abstraction and zap adapter
- `uncommons/log/sanitizer.go`: log-injection prevention

Data and messaging:
- `uncommons/postgres`, `uncommons/mongo`, `uncommons/redis`, `uncommons/rabbitmq`: connector packages

HTTP and server:
- `uncommons/net/http`: Fiber-oriented HTTP helpers and middleware
- `uncommons/net/http/ratelimit`: Redis-backed rate limit storage
- `uncommons/server`: graceful shutdown and lifecycle helpers

Resilience and safety:
- `uncommons/circuitbreaker`: circuit breaker manager and health checker
- `uncommons/backoff`: exponential backoff with jitter
- `uncommons/runtime`: panic recovery, metrics, safe goroutine wrappers
- `uncommons/assert`: production-safe assertions with telemetry
- `uncommons/safe`: panic-free math/regex/slice operations
- `uncommons/security`: sensitive field detection and handling
- `uncommons/errgroup`: goroutine coordination with panic recovery

Domain and support:
- `uncommons/transaction`: typed transaction validation/posting primitives
- `uncommons/crypto`: hashing and symmetric encryption
- `uncommons/jwt`: HMAC-based JWT signing and verification
- `uncommons/license`: license validation and enforcement
- `uncommons/pointers`: pointer conversion helpers
- `uncommons/cron`: cron expression parsing and scheduling
- `uncommons/constants`: shared constants (headers, errors, pagination, etc.)

## API invariants to respect

- Telemetry initialization is explicit with `opentelemetry.NewTelemetry(...)`.
- Global OTEL providers are opt-in via `(*Telemetry).ApplyGlobals()`.
- Metric factory/builder operations return errors and should not be silently ignored.
- Logging uses `uncommons/log.Logger` with 5-method interface: `Log(ctx, level, msg, fields...)`, `With(fields...)`, `WithGroup(name)`, `Enabled(level)`, `Sync(ctx)`. Level constants are `LevelError`, `LevelWarn`, `LevelInfo`, `LevelDebug`.
- Log Field constructors: `String()`, `Int()`, `Bool()`, `Err()` for typed structured fields.
- Zap adapter uses `zap.New(cfg Config)` for construction; `Logger.Raw()` for underlying zap access.
- HTTP helpers in `uncommons/net/http` use `Respond`, `RespondStatus`, `RespondError`, `RenderError`, `FiberErrorHandler` naming. Individual status helpers (BadRequestError, etc.) are removed.
- Reverse proxy uses `ServeReverseProxy(target, policy, res, req) error` with `ReverseProxyPolicy` for SSRF protection.
- Server lifecycle uses `ServerManager` exclusively; `GracefulShutdown` is removed.
- Circuit breaker uses `Manager` interface with `NewManager(logger) (Manager, error)` constructor; `GetOrCreate` returns `(CircuitBreaker, error)` and validates config.
- Assertions use `assert.New(ctx, logger, component, operation)` and return errors instead of panicking.
- Safe operations in `uncommons/safe` use explicit error returns for division, slice access, and regex operations.
- JWT uses `jwt.Parse()` and `jwt.Sign()` with algorithm constants (`AlgHS256`, `AlgHS384`, `AlgHS512`).
- Backoff uses `backoff.ExponentialWithJitter()` and `backoff.WaitContext()`.
- Redis distributed locking uses `NewRedisLockManager()` and `LockManager` interface.
- Mongo client uses `NewClient(ctx, cfg, opts...)` constructor with `Config` type.
- Redis client uses `New(ctx, cfg)` constructor with topology-based `Config` (standalone/sentinel/cluster).
- Postgres client uses `New(cfg Config)` with explicit `Config`; `Resolver(ctx)` replaces `GetDB()`. Migrations via `NewMigrator(cfg)`.
- Transaction flow uses `BuildIntentPlan()` + `ValidateBalanceEligibility()` + `ApplyPosting()` with typed `IntentPlan`, `Posting`, `LedgerTarget`.
- RabbitMQ provides `*Context()` variants of all lifecycle methods; `HealthCheck()` returns `(bool, error)`.
- Redaction uses `Redactor` with `RedactionRule` patterns; `NewDefaultRedactor()` and `NewRedactor(rules, mask)`. Old `FieldObfuscator` interface is removed.

## Coding rules

- Do not add `panic(...)` in production paths.
- Do not swallow errors; return or handle with context.
- Keep exported docs aligned with behavior.
- Reuse existing package patterns before introducing new abstractions.
- Avoid introducing high-cardinality telemetry labels by default.
- Use the v2 log interface (`Log(ctx, level, msg, fields...)`) -- do not add printf-style methods.

## Testing and validation

- Preferred commands:
  - `make test`
  - `make test-unit`
  - `make test-integration`
  - `make lint`
  - `make build`
- For integration tests, Docker is required (`make test-integration`).
- `make lint-fix` for auto-fixing lint issues.

## Migration awareness

- If a task touches renamed/removed v1 symbols, update `MIGRATION_MAP.md`.
- If a task changes package-level behavior or API expectations, update `README.md`.

## Documentation policy

- Keep docs factual and code-backed.
- Avoid speculative roadmap text.
- Prefer concise package-level examples that compile with current API names.
