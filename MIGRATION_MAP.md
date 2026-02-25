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
- New method: `(*Telemetry).Meter(name) (metric.Meter, error)`
- New method: `(*Telemetry).ShutdownTelemetryWithContext(ctx) error` -- context-aware shutdown (alternative to `ShutdownTelemetry()`)
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

**Migration note:** The `organizationID` and `ledgerID` positional parameters and the internal `WithLedgerLabels()` call have been removed. Callers must now pass these labels explicitly via OpenTelemetry attributes:

```go
// v1
factory.RecordAccountCreated(ctx, orgID, ledgerID)

// v2
factory.RecordAccountCreated(ctx,
    attribute.String("organization_id", orgID),
    attribute.String("ledger_id", ledgerID),
)
```

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
- `Any(key, value) Field`
- `String(key, value) Field`
- `Int(key, value) Field`
- `Bool(key, value) Field`
- `Err(err) Field`

### Level constants

- `LevelError` (0), `LevelWarn` (1), `LevelInfo` (2), `LevelDebug` (3), `LevelUnknown` (255)

### GoLogger

`GoLogger` moved from `log.go` to `go_logger.go`, fully reimplemented with the v2 interface. Includes CWE-117 log-injection prevention.

### Sanitizer (package move)

| v1 | v2 |
|----|----|
| `uncommons/logging` package | removed entirely |
| `logging.SafeErrorf(...)` | `log.SafeError(logger, ctx, msg, err, production)` |
| `logging.SanitizeExternalResponse(...)` | `log.SanitizeExternalResponse(statusCode) string` |

---

## uncommons/zap

| v1 | v2 |
|----|----|
| `ZapWithTraceLogger` struct | `Logger` struct (renamed, restructured) |
| `InitializeLoggerWithError() (log.Logger, error)` | removed (use `New(...)`) |
| `InitializeLogger() log.Logger` | removed (use `New(...)`) |
| `InitializeLoggerFromConfig(...)` | `New(cfg Config) (*Logger, error)` |
| `hydrateArgs` / template-based logging | removed |

### New in v2

- New types: `Config`, `Environment` (string type with constants: `EnvironmentProduction`, `EnvironmentStaging`, `EnvironmentUAT`, `EnvironmentDevelopment`, `EnvironmentLocal`)
- `Logger.Raw() *zap.Logger` -- access underlying zap logger
- `Logger.Level() zap.AtomicLevel` -- access dynamic log level
- Direct zap convenience methods: `Debug()`, `Info()`, `Warn()`, `Error()`, `WithZapFields()`
- Field constructors: `Any(key, value)`, `String(key, value)`, `Int(key, value)`, `Bool(key, value)`, `Duration(key, value)`, `ErrorField(err)`

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

**Wire format impact:** `ErrorResponse.Code` changed from `string` to `int`, which changes the JSON serialization from `"code": "400"` to `"code": 400`. Any downstream consumer that unmarshals error responses with `Code` as a string type will break. Callers must update their response parsing structs to use `int` (or a numeric JSON type) for the `code` field.

### Proxy

| v1 | v2 |
|----|----|
| `ServeReverseProxy(target, res, req)` | `ServeReverseProxy(target, policy, res, req) error` |

New: `DefaultReverseProxyPolicy()`, `ReverseProxyPolicy` struct with SSRF protection.

### Pagination (v2 refinement)

| v2 (previous) | v2 (current) |
|---|---|
| `EncodeTimestampCursor(time, uuid) string` | `EncodeTimestampCursor(time, uuid) (string, error)` |
| `EncodeSortCursor(col, val, id, next) string` | `EncodeSortCursor(col, val, id, next) (string, error)` |
| `CalculateSortCursorPagination(...) (next, prev string)` | `CalculateSortCursorPagination(...) (next, prev string, err error)` |
| `ErrOffsetMustBePositive` sentinel | removed (negative offset silently coerced to `DefaultOffset=0`; see note below) |
| `type Order string` + `Asc Order = "asc"` / `Desc Order = "desc"` | removed; replaced by `SortDirASC = "ASC"` / `SortDirDESC = "DESC"` (untyped `string`, uppercase) |

**Migration note (offset coercion):** The `ErrOffsetMustBePositive` sentinel error is removed. In v2, negative offsets are silently coerced to `DefaultOffset=0` instead of returning an error. This tradeoff avoids breaking callers that relied on the previous behavior and preserves backward compatibility. However, callers should validate offsets before calling pagination functions (e.g., reject negative offsets at the handler level) since the pagination codepaths that previously returned `ErrOffsetMustBePositive` will now silently accept any negative value.

**Migration note (cursor/sort):** The cursor encode functions now return errors. The `Order` type is removed; use the `SortDirASC`/`SortDirDESC` constants directly. Note the **case change** from lowercase `"asc"`/`"desc"` to uppercase `"ASC"`/`"DESC"` — any consumer that stores or compares these values must be updated.

New pagination defaults in `constants/pagination.go`: `DefaultLimit=20`, `DefaultOffset=0`, `MaxLimit=200`.

### Handler

| v2 (previous) | v2 (current) |
|---|---|
| `Ping` handler returns `"healthy"` | `Ping` handler returns `"pong"` |

**Migration note:** Any health check monitor that string-matches the response body for `"healthy"` must be updated. Use `HealthWithDependencies` for production health endpoints.

### Health check semantics

| v2 (previous) | v2 (current) |
|---|---|
| `HealthWithDependencies`: HealthCheck overrides CircuitBreaker status | Both must report healthy (AND semantics) |

**Migration note:** An open circuit breaker can no longer be overridden by a passing HealthCheck function. This is the correct reliability behavior but may surface previously-hidden unhealthy states.

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

### New in v2

- `(*ServerManager).WithShutdownTimeout(d) *ServerManager` -- configures max wait for gRPC GracefulStop before hard stop (default: 30s)
- `(*ServerManager).WithShutdownHook(hook func(context.Context) error) *ServerManager` -- registers cleanup callbacks executed during graceful shutdown (nil hooks are silently ignored)
- `(*ServerManager).WithShutdownChannel(ch <-chan struct{}) *ServerManager` -- custom shutdown trigger for tests (instead of relying on OS signals)
- `(*ServerManager).StartWithGracefulShutdownWithError() error` -- returns error on config failure instead of calling `os.Exit(1)`
- `(*ServerManager).ServersStarted() <-chan struct{}` -- closed when server goroutines have been launched (for test coordination)
- `ErrNoServersConfigured` sentinel error

---

## uncommons/mongo

| v1 | v2 |
|----|----|
| `MongoConnection` struct | `Client` struct |
| `BuildConnectionString(scheme, user, password, host, port, parameters, logger) string` | `BuildURI(URIConfig) (string, error)` |
| `MongoConnection{}` + `Connect(ctx)` | `NewClient(ctx, cfg Config, opts ...Option) (*Client, error)` |
| `GetDB(ctx) (*mongo.Client, error)` | `Client(ctx) (*mongo.Client, error)` |
| `EnsureIndexes(ctx, collection, index)` | `EnsureIndexes(ctx, collection, indexes...) error` (variadic) |

### Error sentinels (v2 refinement)

| v2 (previous) | v2 (current) | Notes |
|---|---|---|
| `ErrClientClosed` (nil receiver) | `ErrNilClient` | Nil receiver now returns `ErrNilClient`; `ErrClientClosed` reserved for closed/not-connected state |

### New in v2

- Methods: `Database(ctx)`, `DatabaseName()`, `Ping(ctx)`, `Close(ctx)`, `ResolveClient(ctx)` (alias for `Client(ctx)`)
- Types: `Config`, `URIConfig`, `Option`, `TLSConfig`
- Sentinel errors: `ErrNilClient`, `ErrNilDependency`, `ErrInvalidConfig`, `ErrEmptyURI`, `ErrEmptyDatabaseName`, `ErrEmptyCollectionName`, `ErrEmptyIndexes`, `ErrConnect`, `ErrPing`, `ErrDisconnect`, `ErrCreateIndex`, `ErrNilMongoClient`, `ErrNilContext`
- URI builder errors: `ErrInvalidScheme`, `ErrEmptyHost`, `ErrInvalidPort`, `ErrPortNotAllowedForSRV`, `ErrPasswordWithoutUser`
- `Config.TLS` field — optional `*TLSConfig` for TLS connections (mirrors redis `TLSConfig`)
- Non-TLS connection warning — logs at `Warn` level when connecting without TLS
- `Config.MaxPoolSize` silently clamped to 1000 (mirrors redis `maxPoolSize` pattern)
- Credential clearing — `Config.URI` is cleared after successful `Connect()` to reduce credential exposure

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

### Behavioral changes

| Behavior | v2 |
|----------|-----|
| TLS minimum version | `normalizeTLSDefaults` enforces `tls.VersionTLS12` as the minimum TLS version. Explicit `tls.VersionTLS10` or `tls.VersionTLS11` values in `TLSConfig.MinVersion` are upgraded to TLS 1.2 and a warning is logged. If you still need legacy endpoints temporarily, set `TLSConfig.AllowLegacyMinVersion=true` as an explicit compatibility override and plan removal. |

Recommended rollout:

- First deploy with explicit `TLSConfig.MinVersion=tls.VersionTLS12` where endpoints are compatible.
- Use `TLSConfig.AllowLegacyMinVersion=true` only for temporary exceptions and monitor warning logs.
- Remove legacy override after endpoint upgrades to restore strict floor enforcement.

### Interface and lock handle changes

| v2 (previous) | v2 (current) |
|----|----|
| `TryLock(ctx, key) (*redsync.Mutex, bool, error)` | `TryLock(ctx, key) (LockHandle, bool, error)` |
| `Unlock(ctx, *redsync.Mutex) error` | `LockHandle.Unlock(ctx) error` |
| `DistributedLocker` interface (4 methods, imports `redsync`) | `LockManager` interface (3 methods, no `redsync` dependency) |
| `DistributedLock` struct | `RedisLockManager` struct |
| `NewDistributedLock(conn)` | `NewRedisLockManager(conn) (*RedisLockManager, error)` |

**Migration note:** `TryLock` now returns an opaque `LockHandle` instead of `*redsync.Mutex`. Call `handle.Unlock(ctx)` directly instead of `lock.Unlock(ctx, mutex)`. The standalone `Unlock` method on `DistributedLock` is deprecated -- it now accepts `LockHandle` instead of `*redsync.Mutex`. Consumers no longer need to import `github.com/go-redsync/redsync/v4` to use the `DistributedLocker` interface.

### New in v2

- Config types: `Config`, `Topology`, `StandaloneTopology`, `SentinelTopology`, `ClusterTopology`, `TLSConfig`, `Auth`, `StaticPasswordAuth`, `GCPIAMAuth`, `ConnectionOptions`
- Methods: `GetClient(ctx) (redis.UniversalClient, error)`, `Close() error`, `Status() (Status, error)`, `IsConnected() (bool, error)`, `LastRefreshError() error`
- `SetPackageLogger(log.Logger)` -- configures package-level logger for nil-receiver assertion diagnostics
- `LockHandle` interface -- opaque lock token with self-contained `Unlock(ctx) error`
- `DefaultLockOptions() LockOptions` -- sensible defaults for general-purpose locking
- `RateLimiterLockOptions() LockOptions` -- optimized for rate limiter use case
- `StaticPasswordAuth.String()` / `GCPIAMAuth.String()` -- credential redaction in `fmt` output
- Config validation: `RefreshEvery < TokenLifetime` enforced, `PoolSize` capped at 1000, `LockOptions.Tries` capped at 1000
- Lazy pool adapter: `DistributedLock` survives IAM token refresh reconnections

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

### Error wrapping (v2 refinement)

`SanitizedError.Unwrap()` returns `nil` to prevent error chain traversal from leaking database credentials. `Error()` returns the sanitized text. Because `Unwrap()` is intentionally blocked, `errors.Is/errors.As` do not match the hidden original cause through `SanitizedError`.

### New in v2

- Methods: `Primary() (*sql.DB, error)`, `Close() error`, `IsConnected() (bool, error)`
- Types: `Config`, `MigrationConfig`, `SanitizedError`
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
- New errors: `ErrInsecureTLS`, `ErrNilConnection`, `ErrInsecureHealthCheck`, `ErrHealthCheckHostNotAllowed`, `ErrHealthCheckAllowedHostsRequired`

### Health check rollout/security knobs

- Basic auth over plain HTTP is rejected by default; set `AllowInsecureHealthCheck=true` only as temporary compatibility override.
- Basic-auth health checks now require `HealthCheckAllowedHosts` unless `AllowInsecureHealthCheck=true` is explicitly set.
- Host allowlist controls: `HealthCheckAllowedHosts` (accepts `host` or `host:port`) and `RequireHealthCheckAllowedHosts`.
- Recommended rollout: configure `HealthCheckAllowedHosts` first, then enable `RequireHealthCheckAllowedHosts=true`.

---

## uncommons/outbox/postgres

### Behavioral changes

| Behavior | v2 |
|----------|-----|
| Schema resolver tenant enforcement | `SchemaResolver` now requires tenant context by default. Use `WithAllowEmptyTenant()` only for explicit public-schema/single-tenant flows. |
| Column migration primary key | `migrations/column/000001_outbox_events_column.up.sql` uses composite primary key `(tenant_id, id)` to avoid cross-tenant key coupling. |

---

## uncommons/transaction

### Types restructured

**Removed types:** `Responses`, `Metadata`, `Amount`, `Share`, `Send`, `Source`, `Rate`, `FromTo`, `Distribute`, `Transaction`

**New types:** `Operation`, `TransactionStatus`, `AccountType`, `ErrorCode`, `DomainError`, `LedgerTarget`, `Allocation`, `TransactionIntentInput`, `Posting`, `IntentPlan`

New constructor: `NewDomainError(code, field, message) error`

`Balance` struct changes: removed fields `Alias`, `Key`, `AssetCode`; added field `Asset` (replaces `AssetCode`). `AccountType` changed from `string` to typed `AccountType` enum.

New operation types: `OperationDebit`, `OperationCredit`, `OperationOnHold`, `OperationRelease`
New status types: `StatusCreated`, `StatusApproved`, `StatusPending`, `StatusCanceled`
New function: `ResolveOperation(pending, isSource bool, status TransactionStatus) (Operation, error)`

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
| `NewManager(logger) Manager` | `NewManager(logger, opts...) (Manager, error)` (returns error on nil logger; accepts options) |
| `(*Manager).GetOrCreate(serviceName, config) CircuitBreaker` | `(*Manager).GetOrCreate(serviceName, config) (CircuitBreaker, error)` (validates config) |

New: `Config.Validate() error`
New: `WithMetricsFactory(f *metrics.MetricsFactory) ManagerOption` -- emits `circuit_breaker_state_transitions_total` and `circuit_breaker_executions_total` counters

---

## uncommons/errors

| v1 | v2 |
|----|----|
| `ValidateBusinessError(err, entityType, args...)` | Variadic `args` now appended to error message (previously ignored extra args) |

---

## uncommons/app

| v1 | v2 |
|----|----|
| `(*Launcher).Add(appName, app) *Launcher` | `(*Launcher).Add(appName, app) error` (no more method chaining) |

New sentinel errors: `ErrNilLauncher`, `ErrEmptyApp`, `ErrNilApp`

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

## uncommons/utils

### Signature changes

| v1 | v2 |
|----|----|
| `GenerateUUIDv7() uuid.UUID` | `GenerateUUIDv7() (uuid.UUID, error)` |

**Migration note:** In v1, `GenerateUUIDv7()` internally used `uuid.Must(uuid.NewV7())`, which panics if `crypto/rand` fails. In v2 the panic path is removed: the function returns `(uuid.UUID, error)` so callers can handle the (rare but possible) entropy-source failure gracefully. All call sites must now check the returned error.

### Removed deprecated functions (moved to Midaz)

- `ValidateCountryAddress`, `ValidateAccountType`, `ValidateType`, `ValidateCode`, `ValidateCurrency`
- `GenericInternalKey`, `TransactionInternalKey`, `IdempotencyInternalKey`, `BalanceInternalKey`, `AccountingRoutesInternalKey`

---

## uncommons/crypto

| v1 | v2 |
|----|----|
| `Crypto.Logger` field (`*zap.Logger`) | `Crypto.Logger` field (`log.Logger`) |

Direct `go.uber.org/zap` dependency removed from this package.

---

## uncommons/jwt

### Token validation semantics

| v1 | v2 |
|----|----|
| `Token.Valid` (bool) -- full validation | `Token.SignatureValid` (bool) -- signature-only verification |
| (no separate time validation) | `ValidateTimeClaims(claims) error` |
| (no separate time validation) | `ValidateTimeClaimsAt(claims, now) error` |
| (no combined parse+validate) | `ParseAndValidate(token, secret, allowedAlgs) (*Token, error)` |

**Migration note:** In v1, the `Token.Valid` field was set to `true` after `Parse()` succeeded, which callers commonly interpreted as "the token is fully valid." In v2, `Token.SignatureValid` clarifies that only the cryptographic HMAC signature was verified -- it does **not** cover time-based claims (`exp`, `nbf`, `iat`). Callers relying on `Token.Valid` for authorization decisions must either:

1. Switch to `ParseAndValidate()`, which performs both signature verification and time-claim validation in one call, or
2. Call `ValidateTimeClaims(token.Claims)` (or `ValidateTimeClaimsAt(token.Claims, now)` for deterministic testing) after `Parse()`.

New sentinel errors for time validation: `ErrTokenExpired`, `ErrTokenNotYetValid`, `ErrTokenIssuedInFuture`.

---

## uncommons/license

| v1 | v2 |
|----|----|
| `DefaultHandler(reason)` panics | `DefaultHandler(reason)` records assertion failure (no panic) |
| `ManagerShutdown.Terminate(reason)` panics on nil handler | Records assertion failure, returns without panic |
| Direct struct construction `&ManagerShutdown{}` | `New(opts ...ManagerOption) *ManagerShutdown` constructor with functional options |

### New in v2

- `New(opts ...ManagerOption) *ManagerShutdown` -- constructor with default handler and functional options
- `WithLogger(l log.Logger) ManagerOption` -- provides structured logger for assertion and validation logging
- `DefaultHandlerWithError(reason string) error` -- returns `ErrLicenseValidationFailed` instead of panicking
- `(*ManagerShutdown).TerminateWithError(reason) error` -- returns error instead of invoking handler (for validation checks)
- `(*ManagerShutdown).TerminateSafe(reason) error` -- invokes handler but returns error if manager is uninitialized
- Sentinel errors: `ErrLicenseValidationFailed`, `ErrManagerNotInitialized`

---

## uncommons/cron

| v1 | v2 |
|----|----|
| `schedule.Next(from)` on nil receiver | returns `(time.Time{}, nil)` -> now returns `(time.Time{}, ErrNilSchedule)` |

New error: `ErrNilSchedule`

---

## uncommons/security

| v1 | v2 |
|----|----|
| `DefaultSensitiveFieldsMap()` | still available (reimplemented with lazy init + `sync.Once`) |

Field list expanded with additional financial and PII identifiers.

---

## New packages in v2

### uncommons/circuitbreaker

- `NewManager(logger, opts...) (Manager, error)` -- circuit breaker manager for service-level resilience
- `WithMetricsFactory(f *metrics.MetricsFactory) ManagerOption` -- emits state transition and execution counters
- `NewHealthCheckerWithValidation(manager, interval, timeout, logger) (HealthChecker, error)` -- periodic health checks with recovery and config validation
- Preset configs: `DefaultConfig()`, `AggressiveConfig()`, `ConservativeConfig()`, `HTTPServiceConfig()`, `DatabaseConfig()`
- `Config.Validate() error` -- validates circuit breaker configuration
- Core types: `Config`, `State`, `Counts`, `CircuitBreaker` interface, `Manager` interface, `HealthChecker` interface
- State constants: `StateClosed`, `StateOpen`, `StateHalfOpen`, `StateUnknown`
- Sentinel errors: `ErrInvalidConfig`, `ErrNilLogger`, `ErrNilCircuitBreaker`, `ErrNilManager`, `ErrInvalidHealthCheckInterval`, `ErrInvalidHealthCheckTimeout`

### uncommons/assert

- `New(ctx, logger, component, operation) *Asserter` -- production-safe assertions
- Methods: `That()`, `NotNil()`, `NotEmpty()`, `NoError()`, `Never()`, `Halt()`
- Returns errors + emits telemetry instead of panicking
- Metrics: `InitAssertionMetrics(factory)`, `GetAssertionMetrics()`, `ResetAssertionMetrics()`
- Predicates library (`predicates.go`): `Positive`, `NonNegative`, `NotZero`, `InRange`, `PositiveInt`, `InRangeInt`, `ValidUUID`, `ValidAmount`, `ValidScale`, `PositiveDecimal`, `NonNegativeDecimal`, `ValidPort`, `ValidSSLMode`, `DebitsEqualCredits`, `NonZeroTotals`, `ValidTransactionStatus`, `TransactionCanTransitionTo`, `TransactionCanBeReverted`, `BalanceSufficientForRelease`, `DateNotInFuture`, `DateAfter`, `BalanceIsZero`, `TransactionHasOperations`, `TransactionOperationsMatch`
- Sentinel error: `ErrAssertionFailed`

### uncommons/runtime

- Recovery: `RecoverAndLog`, `RecoverAndCrash`, `RecoverWithPolicy` (and `*WithContext` variants)
- Safe goroutines: `SafeGo`, `SafeGoWithContext`, `SafeGoWithContextAndComponent` with `PanicPolicy` (KeepRunning/CrashProcess)
- Panic metrics: `InitPanicMetrics(factory[, logger])`, `GetPanicMetrics()`, `ResetPanicMetrics()`
- Span recording: `RecordPanicToSpan`, `RecordPanicToSpanWithComponent`
- Error reporter: `SetErrorReporter(reporter)`, `GetErrorReporter()` with `ErrorReporter` interface
- Production mode: `SetProductionMode(bool)`, `IsProductionMode() bool`
- Sentinel error: `ErrPanic`

### uncommons/safe

- **Math:** `Divide()`, `DivideRound()`, `DivideOrZero()`, `DivideOrDefault()`, `Percentage()`, `PercentageOrZero()` on `decimal.Decimal` with zero-division safety; `DivideFloat64()`, `DivideFloat64OrZero()` for float64
- **Regex:** `Compile()`, `CompilePOSIX()`, `MatchString()`, `FindString()`, `ClearCache()` with caching
- **Slices:** `First[T]()`, `Last[T]()`, `At[T]()` with error returns and `*OrDefault` variants
- Sentinel errors: `ErrDivisionByZero`, `ErrInvalidRegex`, `ErrEmptySlice`, `ErrIndexOutOfBounds`

### uncommons/security

- `IsSensitiveField(name) bool` -- case-insensitive sensitive field detection
- `DefaultSensitiveFields() []string` -- default sensitive field patterns
- `DefaultSensitiveFieldsMap() map[string]bool` -- map version for lookups

### uncommons/jwt

- `Parse(token, secret, allowedAlgs) (*Token, error)` -- HMAC JWT signature verification only
- `ParseAndValidate(token, secret, allowedAlgs) (*Token, error)` -- signature + time claim validation
- `Sign(claims, secret, alg) (string, error)` -- HMAC JWT creation
- `ValidateTimeClaims(claims) error` -- exp/nbf/iat validation against current UTC time
- `ValidateTimeClaimsAt(claims, now) error` -- exp/nbf/iat validation against a specific time (for deterministic testing)
- `Token.SignatureValid` (bool) -- replaces v1 `Token.Valid`; clarifies signature-only scope
- Algorithms: `AlgHS256`, `AlgHS384`, `AlgHS512`
- Sentinel errors: `ErrTokenExpired`, `ErrTokenNotYetValid`, `ErrTokenIssuedInFuture`

### uncommons/backoff

- `Exponential(base, attempt) time.Duration` -- exponential delay calculation
- `FullJitter(delay) time.Duration` -- crypto/rand-based jitter
- `ExponentialWithJitter(base, attempt) time.Duration` -- combined helper
- `WaitContext(ctx, delay) error` -- context-aware sleep (renamed from `SleepWithContext`)

### uncommons/pointers

- `String()`, `Bool()`, `Time()`, `Int()`, `Int64()`, `Float64()` -- value-to-pointer helpers

### uncommons/cron

- `Parse(expr) (Schedule, error)` -- 5-field cron expression parser
- `Schedule.Next(t) (time.Time, error)` -- next execution time

### uncommons/errgroup

- `WithContext(ctx) (*Group, context.Context)` -- goroutine group with cancellation
- `(*Group).Go(fn)` -- launch goroutine with panic recovery
- `(*Group).Wait() error` -- wait and return first error
- `(*Group).SetLogger(logger)` -- configure logger for panic recovery diagnostics
- Sentinel error: `ErrPanicRecovered`

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

# Check for v1 patterns that changed signature or semantics in v2
rg -n "uuid\.Must\(uuid\.NewV7|GenerateUUIDv7\(\)" . --type go  # should now return (uuid.UUID, error)
rg -n "Token\.Valid\b" . --type go                                # renamed to Token.SignatureValid
rg -n "\"code\":\s*\"[0-9]" . --type go                          # ErrorResponse.Code is now int, not string

# Check for new v2 packages
rg -n "uncommons/circuitbreaker|uncommons/assert|uncommons/safe|uncommons/security|uncommons/jwt|uncommons/backoff|uncommons/pointers|uncommons/cron|uncommons/errgroup" . --type go
```
