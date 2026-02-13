# lib-uncommons Migration Map (v2)

This document maps every notable v1 API path (from the `main` branch) to the current v2 APIs (in the `develop` branch and unstaged working tree). Use it as a lookup reference when migrating consumer code from v1 to v2.

---

## uncommons/opentelemetry

### Initialization

| v1 | v2 | Notes |
|----|----|----|
| `InitializeTelemetryWithError(*TelemetryConfig)` | `NewTelemetry(TelemetryConfig) (*Telemetry, error)` | Config passed by value, not pointer |
| `InitializeTelemetry(*TelemetryConfig)` | removed | Use `NewTelemetry` (no silent-failure variant) |
| implicit globals on init | explicit `(*Telemetry).ApplyGlobals()` | Globals are opt-in now |

### Span helpers (pointer -> value receivers on span)

| v1 | v2 |
|----|----|
| `HandleSpanError(*trace.Span, ...)` | `HandleSpanError(trace.Span, ...)` |
| `HandleSpanEvent(*trace.Span, ...)` | `HandleSpanEvent(trace.Span, ...)` |
| `HandleSpanBusinessErrorEvent(*trace.Span, ...)` | `HandleSpanBusinessErrorEvent(trace.Span, ...)` |

### Span attributes

| v1 | v2 |
|----|----|
| `SetSpanAttributesFromStruct(...)` | removed; use `SetSpanAttributesFromValue(...)` |
| `SetSpanAttributesFromStructWithObfuscation(...)` | removed; use `SetSpanAttributesFromValue(...)` |
| `SetSpanAttributesFromStructWithCustomObfuscation(...)` | removed; use `SetSpanAttributesFromValue(...)` |

### Struct and field changes

| v1 | v2 |
|----|----|
| `Telemetry.MetricProvider` field | renamed to `Telemetry.MeterProvider` |
| `ErrNilTelemetryConfig` | removed; replaced by `ErrNilTelemetryLogger`, `ErrEmptyEndpoint`, `ErrNilTelemetry`, `ErrNilShutdown` |

### New in v2

- `TelemetryConfig` gains fields: `InsecureExporter bool`, `Propagator propagation.TextMapPropagator`, `Redactor *Redactor`
- New method: `(*Telemetry).Tracer(name) (trace.Tracer, error)`
- New type: `RedactingAttrBagSpanProcessor` (span processor that redacts sensitive span attributes)

### Obfuscation -> Redaction

The entire obfuscation subsystem has been replaced by the redaction subsystem.

| v1 | v2 |
|----|----|
| `FieldObfuscator` interface | removed entirely |
| `DefaultObfuscator` struct | removed |
| `CustomObfuscator` struct | removed |
| `NewDefaultObfuscator()` | `NewDefaultRedactor()` |
| `NewCustomObfuscator([]string)` | `NewRedactor([]RedactionRule, maskValue)` |
| `ObfuscateStruct(any, FieldObfuscator)` | `ObfuscateStruct(any, *Redactor)` |

New types:

- `RedactionAction` (string type)
- `RedactionRule` struct
- `Redactor` struct
- Constants: `RedactionMask`, `RedactionHash`, `RedactionDrop`

### Propagation

All propagation functions now follow the `context-first` convention.

| v1 | v2 |
|----|----|
| `InjectHTTPContext(*http.Header, context.Context)` | `InjectHTTPContext(context.Context, http.Header)` |
| `ExtractHTTPContext(*fiber.Ctx)` | `ExtractHTTPContext(context.Context, *fiber.Ctx)` |
| `InjectGRPCContext(context.Context)` | `InjectGRPCContext(context.Context, metadata.MD) metadata.MD` |
| `ExtractGRPCContext(context.Context)` | `ExtractGRPCContext(context.Context, metadata.MD) context.Context` |

New low-level APIs:

- `InjectTraceContext(context.Context, propagation.TextMapCarrier)`
- `ExtractTraceContext(context.Context, propagation.TextMapCarrier) context.Context`

---

## uncommons/opentelemetry/metrics

### Factory and builders now return errors

| v1 | v2 |
|----|----|
| `NewMetricsFactory(meter, logger) *MetricsFactory` | `NewMetricsFactory(meter, logger) (*MetricsFactory, error)` |
| `(*MetricsFactory).Counter(m) *CounterBuilder` | `(*MetricsFactory).Counter(m) (*CounterBuilder, error)` |
| `(*MetricsFactory).Gauge(m) *GaugeBuilder` | `(*MetricsFactory).Gauge(m) (*GaugeBuilder, error)` |
| `(*MetricsFactory).Histogram(m) *HistogramBuilder` | `(*MetricsFactory).Histogram(m) (*HistogramBuilder, error)` |

### Builder operations now return errors

| v1 | v2 |
|----|----|
| `(*CounterBuilder).Add(ctx, value)` | now returns `error` |
| `(*CounterBuilder).AddOne(ctx)` | now returns `error` |
| `(*GaugeBuilder).Set(ctx, value)` | now returns `error` |
| `(*GaugeBuilder).Record(ctx, value)` | removed (was deprecated; use `Set`) |
| `(*HistogramBuilder).Record(ctx, value)` | now returns `error` |

### Removed label helpers

| v1 | v2 |
|----|----|
| `WithOrganizationLabels(...)` | removed |
| `WithLedgerLabels(...)` | removed |

### Convenience recorders (organization/ledger args removed)

| v1 | v2 |
|----|----|
| `RecordAccountCreated(ctx, organizationID, ledgerID, attrs...)` | `RecordAccountCreated(ctx, attrs...) error` |
| `RecordTransactionProcessed(ctx, organizationID, ledgerID, attrs...)` | `RecordTransactionProcessed(ctx, attrs...) error` |
| `RecordOperationRouteCreated(ctx, organizationID, ledgerID, attrs...)` | `RecordOperationRouteCreated(ctx, attrs...) error` |
| `RecordTransactionRouteCreated(ctx, organizationID, ledgerID, attrs...)` | `RecordTransactionRouteCreated(ctx, attrs...) error` |

### New in v2

- `NewNopFactory() *MetricsFactory` -- no-op fallback for tests / disabled metrics
- New sentinel errors: `ErrNilMeter`, `ErrNilCounter`, `ErrNilGauge`, `ErrNilHistogram`

---

## uncommons/log

### Interface rewrite (18 methods -> 5)

The `Logger` interface has been completely redesigned.

**v1 interface (18 methods):**

```
Info / Infof / Infoln
Error / Errorf / Errorln
Warn / Warnf / Warnln
Debug / Debugf / Debugln
Fatal / Fatalf / Fatalln
WithFields(fields ...any) Logger
WithDefaultMessageTemplate(message string) Logger
Sync() error
```

**v2 interface (5 methods):**

```
Log(ctx context.Context, level Level, msg string, fields ...Field)
With(fields ...Field) Logger
WithGroup(name string) Logger
Enabled(level Level) bool
Sync(ctx context.Context) error
```

### Level type and constants

| v1 | v2 |
|----|----|
| `LogLevel` type (int8) | `Level` type (uint8) |
| `PanicLevel` | removed entirely |
| `FatalLevel` | removed entirely |
| `ErrorLevel` | `LevelError` |
| `WarnLevel` | `LevelWarn` |
| `InfoLevel` | `LevelInfo` |
| `DebugLevel` | `LevelDebug` |
| `ParseLevel(string) (LogLevel, error)` | `ParseLevel(string) (Level, error)` (no longer accepts "panic" or "fatal") |

### Logger helpers

| v1 | v2 |
|----|----|
| `NoneLogger` | `NopLogger` |
| (no constructor) | `NewNop() Logger` |
| `WithFields(fields ...any) Logger` | `With(fields ...Field) Logger` |
| `WithDefaultMessageTemplate(message string) Logger` | removed |
| `Sync() error` | `Sync(ctx context.Context) error` |

### New `Field` type

v2 introduces a structured `Field` type with constructors:

- `Field` struct: `Key string`, `Value any`
- `String(key, value) Field`
- `Int(key, value) Field`
- `Bool(key, value) Field`
- `Err(err) Field`

### GoLogger

`GoLogger` moved from `log.go` to `go_logger.go`, fully reimplemented with the v2 interface. Includes CWE-117 log-injection prevention.

### Sanitizer (package move)

| v1 | v2 |
|----|----|
| `uncommons/logging` package | removed entirely |
| `logging.SafeErrorf(...)` | `log.SafeErrorf(...)` |
| `logging.SanitizeExternalResponse(...)` | `log.SanitizeExternalResponse(...)` |

New: `log.SetProductionModeResolver(fn)` -- injectable production mode detection.

---

## uncommons/zap

| v1 | v2 |
|----|----|
| `ZapWithTraceLogger` struct | `Logger` struct (renamed, restructured) |
| `InitializeLoggerWithError() (log.Logger, error)` | removed (use `New(...)`) |
| `InitializeLogger() log.Logger` | removed (use `New(...)`) |
| `InitializeLoggerFromConfig(...)` | `New(cfg Config) (*Logger, zap.AtomicLevel, error)` |
| `hydrateArgs` / template-based logging | removed |

### New in v2

- New types: `Config`, `Environment` (string type with constants)
- `Logger.Raw() *zap.Logger` -- access underlying zap logger
- Field constructors: `Any(key, value)`, `String(key, value)`, `Int(key, value)`, `Bool(key, value)`, `Duration(key, value)`

---

## uncommons/net/http

### Response helpers consolidated

All individual status helpers have been removed in favor of two generic functions.

| v1 | v2 |
|----|----|
| `WriteError(c, status, title, message)` | `RespondError(c, status, title, message)` |
| `HandleFiberError(c, err)` | `FiberErrorHandler(c, err)` |
| `JSONResponse(c, status, s)` | `Respond(c, status, payload)` |
| `JSONResponseError(c, err)` | removed (use `RespondError`) |
| `NoContent(c)` | `RespondStatus(c, status)` |

**Removed individual status helpers** (use `Respond` / `RespondError` / `RespondStatus` instead):

`BadRequestError`, `UnauthorizedError`, `ForbiddenError`, `NotFoundError`, `ConflictError`, `RequestEntityTooLargeError`, `UnprocessableEntityError`, `SimpleInternalServerError`, `InternalServerErrorWithTitle`, `ServiceUnavailableError`, `ServiceUnavailableErrorWithTitle`, `GatewayTimeoutError`, `GatewayTimeoutErrorWithTitle`, `Unauthorized`, `Forbidden`, `BadRequest`, `Created`, `OK`, `Accepted`, `PartialContent`, `RangeNotSatisfiable`, `NotFound`, `Conflict`, `NotImplemented`, `UnprocessableEntity`, `InternalServerError`

### Cursor pagination

| v1 | v2 |
|----|----|
| `Cursor.PointsNext` (bool) | `Cursor.Direction` (string: `"next"` / `"prev"`) |
| `CreateCursor(id, pointsNext)` | removed (construct `Cursor` directly) |
| `ApplyCursorPagination(squirrel.SelectBuilder, ...)` | removed (use `CursorDirectionRules(sortDir, cursorDir)`) |
| `PaginateRecords[T](..., pointsNext bool, ..., orderUsed string)` | `PaginateRecords[T](..., cursorDirection string, ...) ` (orderUsed removed) |
| `CalculateCursor(..., pointsNext bool, ...)` | `CalculateCursor(..., cursorDirection string, ...)` |
| `EncodeCursor(cursor) string` | `EncodeCursor(cursor) (string, error)` (now validates) |

New constants: `CursorDirectionNext`, `CursorDirectionPrev`
New error: `ErrInvalidCursorDirection`

### Validation / context

| v1 | v2 |
|----|----|
| `ParseAndVerifyContextParam(...)` | `ParseAndVerifyTenantScopedID(...)` |
| `ParseAndVerifyContextQuery(...)` | `ParseAndVerifyResourceScopedID(...)` |
| `ParseAndVerifyExceptionParam(...)` | removed |
| `ParseAndVerifyDisputeParam(...)` | removed |
| `ContextOwnershipVerifier` interface | `TenantOwnershipVerifier` func type |
| `ExceptionOwnershipVerifier` interface | removed |
| `DisputeOwnershipVerifier` interface | removed |

New types: `ResourceOwnershipVerifier` func type, `IDLocation` type, `ErrInvalidIDLocation`, `ErrLookupFailed`

### Error types

| v1 | v2 |
|----|----|
| `ErrorResponse.Code` (string) | `ErrorResponse.Code` (int) |
| `ErrorResponse.Error` field | removed |
| `WithError(ctx, err)` | `RenderError(ctx, err)` |
| `HealthSimple` var | removed (use `Ping` directly) |

`ErrorResponse` now implements the `error` interface.

### Proxy

| v1 | v2 |
|----|----|
| `ServeReverseProxy(target, res, req)` | `ServeReverseProxy(target, policy, res, req) error` |

New: `DefaultReverseProxyPolicy()`, `ReverseProxyPolicy` struct with SSRF protection.

### Rate limit storage

| v1 | v2 |
|----|----|
| `NewRedisStorage(conn *RedisConnection)` | `NewRedisStorage(conn *Client)` |
| Nil storage operations silently return nil | Now return `ErrStorageUnavailable` |

---

## uncommons/server

| v1 | v2 |
|----|----|
| `GracefulShutdown` struct | removed entirely |
| `NewGracefulShutdown(...)` | removed |
| `(*GracefulShutdown).HandleShutdown()` | removed |

Use `ServerManager` (already existed in v1) with `StartWithGracefulShutdown()`.

New: `(*ServerManager).WithShutdownTimeout(d) *ServerManager`

---

## uncommons/mongo

| v1 | v2 |
|----|----|
| `MongoConnection` struct | `Client` struct |
| `BuildConnectionString(scheme, user, password, host, port, parameters, logger) string` | `BuildURI(URIConfig) (string, error)` |
| `MongoConnection{}` + `Connect(ctx)` | `NewClient(ctx, cfg Config, opts ...Option) (*Client, error)` |
| `GetDB(ctx) (*mongo.Client, error)` | `Client(ctx) (*mongo.Client, error)` |
| `EnsureIndexes(ctx, collection, index)` | `EnsureIndexes(ctx, collection, indexes...) error` (variadic) |

### New in v2

- Methods: `Database(ctx)`, `DatabaseName()`, `Ping(ctx)`, `Close(ctx)`
- Types: `Config`, `URIConfig`, `Option`

---

## uncommons/redis

| v1 | v2 |
|----|----|
| `RedisConnection` struct | `Client` struct |
| `Mode` type | removed |
| `RedisConnection{}` + `Connect(ctx)` | `New(ctx, cfg Config) (*Client, error)` |
| `NewDistributedLock(conn *RedisConnection)` | `NewDistributedLock(conn *Client)` |
| `WithLock(ctx, key, func() error)` | `WithLock(ctx, key, func(context.Context) error)` (context propagated to callback) |
| `WithLockOptions(ctx, key, opts, func() error)` | `WithLockOptions(ctx, key, opts, func(context.Context) error)` |
| `InitVariables()` | removed (handled by constructor) |
| `BuildTLSConfig()` | removed (handled internally) |

### New in v2

- Config types: `Config`, `Topology`, `StandaloneTopology`, `SentinelTopology`, `ClusterTopology`, `TLSConfig`, `Auth`, `StaticPasswordAuth`, `GCPIAMAuth`, `ConnectionOptions`
- Methods: `Close() error`, `Status() (Status, error)`, `IsConnected() (bool, error)`, `LastRefreshError() error`

---

## uncommons/postgres

| v1 | v2 |
|----|----|
| `PostgresConnection` struct | `Client` struct |
| `PostgresConnection{}` + field assignment | `New(cfg Config) (*Client, error)` |
| `Connect() error` | `Connect(ctx context.Context) error` |
| `GetDB() (dbresolver.DB, error)` | `Resolver(ctx context.Context) (dbresolver.DB, error)` |
| `Pagination` struct | removed (moved to `uncommons/net/http`) |
| `squirrel` dependency | removed |

### New in v2

- Methods: `Primary() (*sql.DB, error)`, `Close() error`, `IsConnected() (bool, error)`
- Types: `Config`, `MigrationConfig`
- Migration: `NewMigrator(cfg MigrationConfig) (*Migrator, error)` and `(*Migrator).Up(ctx) error`

---

## uncommons/rabbitmq

### Context-aware methods added alongside existing ones

| Existing (kept) | New context-aware variant |
|----|----|
| `Connect()` | `ConnectContext(ctx) error` |
| `EnsureChannel()` | `EnsureChannelContext(ctx) error` |
| `GetNewConnect()` | `GetNewConnectContext(ctx) (*amqp.Channel, error)` |

### Changed signatures

| v1 | v2 |
|----|----|
| `HealthCheck() bool` | `HealthCheck() (bool, error)` (now returns error) |

### New in v2

- `HealthCheckContext(ctx) (bool, error)`
- `Close() error`, `CloseContext(ctx) error`
- New errors: `ErrInsecureTLS`, `ErrNilConnection`

---

## uncommons/transaction

### Types restructured

**Removed types:** `Responses`, `Metadata`, `Amount`, `Share`, `Send`, `Source`, `Rate`, `FromTo`, `Distribute`, `Transaction`

**New types:** `Operation`, `TransactionStatus`, `AccountType`, `ErrorCode`, `DomainError`, `LedgerTarget`, `Allocation`, `TransactionIntentInput`, `Posting`, `IntentPlan`

New constructor: `NewDomainError(code, field, message) error`

### Validation flow

| v1 | v2 |
|----|----|
| `ValidateBalancesRules(ctx, transaction, validate, balances) error` | `BuildIntentPlan(input, status) (IntentPlan, error)` + `ValidateBalanceEligibility(plan, balances) error` |
| `ValidateFromToOperation(ft, validate, balance) (Amount, Balance, error)` | `ApplyPosting(balance, posting) (Balance, error)` |

**Removed helpers:** `SplitAlias`, `ConcatAlias`, `AliasKey`, `SplitAliasWithKey`, `OperateBalances`

---

## uncommons/circuitbreaker

| v1 | v2 |
|----|----|
| `NewManager(logger) Manager` | `NewManager(logger) (Manager, error)` (returns error on nil logger) |
| `(*Manager).GetOrCreate(serviceName, config) CircuitBreaker` | `(*Manager).GetOrCreate(serviceName, config) (CircuitBreaker, error)` (validates config) |

New: `Config.Validate() error`

---

## uncommons/errors

| v1 | v2 |
|----|----|
| `ValidateBusinessError(err, entityType, args...)` | Variadic `args` now appended to error message (previously ignored extra args) |

---

## uncommons/context (removals)

| v1 | v2 |
|----|----|
| `NewTracerFromContext(ctx)` | removed (was deprecated; use `NewTrackingFromContext`) |
| `NewMetricFactoryFromContext(ctx)` | removed (was deprecated; use `NewTrackingFromContext`) |
| `NewHeaderIDFromContext(ctx)` | removed (was deprecated; use `NewTrackingFromContext`) |
| `WithTimeout(parent, timeout)` | removed (was deprecated; use `WithTimeoutSafe`) |
| All `NoneLogger{}` references | `NopLogger{}` |

---

## uncommons/os

| v1 | v2 |
|----|----|
| `EnsureConfigFromEnvVars(s any) any` | removed (use `SetConfigFromEnvVars(s any) error`) |

---

## uncommons/security

| v1 | v2 |
|----|----|
| `DefaultSensitiveFieldsMap()` | still available (reimplemented with lazy init + `sync.Once`) |

Field list expanded with additional financial and PII identifiers.

---

## New packages in v2

### uncommons/circuitbreaker

- `NewManager(logger) (Manager, error)` -- circuit breaker manager for service-level resilience
- `NewHealthChecker(config) HealthChecker` -- periodic health checks with recovery
- Core types: `Config`, `State`, `Counts`, `CircuitBreaker` interface

### uncommons/assert

- `New(ctx, logger, component, operation) *Asserter` -- production-safe assertions
- Methods: `That()`, `NotNil()`, `NotEmpty()`, `NoError()`, `Never()`, `Halt()`
- Returns errors + emits telemetry instead of panicking

### uncommons/safe

- **Regex:** `Compile()`, `CompilePOSIX()`, `MatchString()`, `FindString()` with caching
- **Slices:** `First[T]()`, `Last[T]()`, `At[T]()` with error returns and `*OrDefault` variants
- **Math:** `Divide()`, `DivideRound()`, `DivideOrZero()`, `DivideOrDefault()`, `Percentage()`, `PercentageOrZero()` on `decimal.Decimal` with zero-division safety

### uncommons/security

- `IsSensitiveField(name) bool` -- case-insensitive sensitive field detection
- `DefaultSensitiveFields() []string` -- default sensitive field patterns
- `DefaultSensitiveFieldsMap() map[string]bool` -- map version for lookups

### uncommons/jwt

- `Parse(token, secret, allowedAlgs) (*Token, error)` -- HMAC JWT verification
- `Sign(claims, secret, alg) (string, error)` -- HMAC JWT creation
- `ValidateTimeClaims(claims, now) error` -- exp/nbf/iat validation
- Algorithms: `AlgHS256`, `AlgHS384`, `AlgHS512`

### uncommons/backoff

- `Exponential(base, attempt) time.Duration` -- exponential delay calculation
- `FullJitter(delay) time.Duration` -- crypto/rand-based jitter
- `ExponentialWithJitter(base, attempt) time.Duration` -- combined helper
- `WaitContext(ctx, delay) error` -- context-aware sleep

### uncommons/pointers

- `String()`, `Bool()`, `Time()`, `Int()`, `Int64()`, `Float64()` -- value-to-pointer helpers

### uncommons/cron

- `Parse(expr) (Schedule, error)` -- 5-field cron expression parser
- `Schedule.Next(t) (time.Time, error)` -- next execution time

### uncommons/errgroup

- `WithContext(ctx) (*Group, context.Context)` -- goroutine group with cancellation
- `(*Group).Go(fn)` -- launch goroutine with panic recovery
- `(*Group).Wait() error` -- wait and return first error

---

## Deleted files in v2

The following files were removed during v2 consolidation:

| File | Reason |
|------|--------|
| `mk/tests.mk` | test targets inlined into main Makefile |
| `uncommons/logging/sanitizer.go` + `sanitizer_test.go` | package removed; moved to `uncommons/log/sanitizer.go` |
| `uncommons/opentelemetry/metrics/labels.go` | organization/ledger label helpers removed |
| `uncommons/opentelemetry/metrics/metrics_test.go` | replaced by v2 test suite |
| `uncommons/opentelemetry/otel_test.go` | replaced by v2 test suite |
| `uncommons/opentelemetry/extract_queue_test.go` | consolidated |
| `uncommons/opentelemetry/inject_trace_test.go` | consolidated |
| `uncommons/opentelemetry/queue_trace_test.go` | consolidated |
| `uncommons/postgres/pagination.go` | `Pagination` moved to `uncommons/net/http` |
| `uncommons/runtime/log_mode_link.go` | functionality inlined into runtime package |
| `uncommons/server/grpc_test.go` | removed |
| `uncommons/zap/sanitize.go` + `sanitize_test.go` | CWE-117 sanitization moved into zap core |

---

## Suggested verification command

```bash
# Check for removed v1 patterns
rg -n "InitializeTelemetryWithError|InitializeTelemetry\(|SetSpanAttributesFromStruct|WithLedgerLabels|WithOrganizationLabels|NoneLogger|BuildConnectionString\(|WriteError\(|HandleFiberError\(|ValidateBalancesRules\(|DetermineOperation\(|ValidateFromToOperation\(|NewTracerFromContext\(|NewMetricFactoryFromContext\(|NewHeaderIDFromContext\(|EnsureConfigFromEnvVars\(|WithTimeout\(|GracefulShutdown|MongoConnection|PostgresConnection|RedisConnection|ZapWithTraceLogger|FieldObfuscator|LogLevel|NoneLogger|WithFields\(|InitializeLogger\b" .

# Check for new v2 packages
rg -n "uncommons/circuitbreaker|uncommons/assert|uncommons/safe|uncommons/security|uncommons/jwt|uncommons/backoff|uncommons/pointers|uncommons/cron|uncommons/errgroup" . --type go
```
