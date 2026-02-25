# Coming from lib-commons

This guide is for engineers migrating from `lib-commons` to `lib-uncommons`.  
It covers every meaningful difference: new packages, redesigned APIs, breaking changes, and the design principles behind each decision.

> **Portuguese version:** [COMING_FROM_COMMONS_pt.md](COMING_FROM_COMMONS_pt.md)

---

## Table of Contents

1. [Module and Namespace](#1-module-and-namespace)
2. [New Packages](#2-new-packages)
3. [Redesigned Packages](#3-redesigned-packages)
   - [log](#log--interface-redesigned-breaking)
   - [opentelemetry](#opentelemetry--redaction-explicit-types-clean-api)
   - [opentelemetry/metrics](#otelmetrics--error-returns-nopfactory-no-positional-org-ledger-args)
   - [circuitbreaker](#circuitbreaker--validation-metrics-safe-health-checker)
   - [redis](#redis--declarative-topology-gcp-iam-typed-lock-api)
   - [postgres](#postgres--sanitizederror-dsn-validation-migration-safety)
   - [mongo](#mongo--backoff-rate-limiting-tls-uri-builder)
   - [server](#server--expanded-api-hooks-serversstarted-channel)
   - [net/http](#nethttp--unified-api-ssrf-cursor-types-ownership-verification)
   - [rabbitmq](#rabbitmq--context-aware-tls-safety-credential-sanitization)
   - [crypto](#crypto--nil-safety-credential-redaction)
   - [license](#license--no-panic-more-termination-options)
   - [constants](#constants--new-groups-utility-function)
4. [Cross-Cutting Patterns](#4-cross-cutting-patterns)
5. [Intentional Breaking Changes (Removals)](#5-intentional-breaking-changes-removals)
6. [Design Principles Summary](#6-design-principles-summary)

---

## 1. Module and Namespace

| | lib-commons | lib-uncommons |
|---|---|---|
| Module path | `github.com/LerianStudio/lib-commons` | `github.com/LerianStudio/lib-uncommons/v2` |
| Package prefix | `commons/` | `uncommons/` |
| Go version | 1.23.x | 1.25.7 |
| Explicit versioning | none | `/v2` in module path |

The `/v2` suffix in the module path signals that `lib-uncommons` manages breaking changes explicitly and allows consumers to run both libraries in parallel during migration.

---

## 2. New Packages

These packages have **no equivalent in lib-commons**. They cover gaps that previously forced each service to re-implement the same patterns from scratch — accumulating inconsistencies and potential security bugs.

### `uncommons/assert` — Production-Safe Assertions with Telemetry

Replaces ad-hoc `if err != nil { panic(...) }` guards with structured, observable assertions that never crash the process.

```go
// lib-commons: no equivalent — services wrote their own guards
if amount.IsZero() {
    panic("amount cannot be zero")
}

// lib-uncommons
a := assert.New(ctx, logger, "payment-service", "ProcessPayment")
if err := a.That(ctx, assert.ValidAmount(amount), "amount must be positive"); err != nil {
    return err
}
```

Key capabilities:
- Methods: `That`, `NotNil`, `NotEmpty`, `NoError`, `Never`, `Halt`
- Every failure automatically: increments `assertion_failed_total` OTEL counter, adds a span event, calls `span.RecordError()`, and sets span status to Error
- Stack traces are suppressed when `IsProductionMode()` returns true — prevents sensitive data leaking into logs
- `Halt(err)` calls `runtime.Goexit()` to exit only the current goroutine without crashing the process
- **Domain predicate library** (`predicates.go`): ~20 typed predicates for financial domain — `DebitsEqualCredits`, `TransactionCanTransitionTo`, `BalanceSufficientForRelease`, `ValidAmount`, `ValidUUID`, and more

### `uncommons/backoff` — Exponential Backoff with Full Jitter

Previously backoff logic was embedded inside individual connectors and was not reusable.

```go
// lib-uncommons — standalone, reusable
for attempt := 0; attempt < maxAttempts; attempt++ {
    if err := doSomething(ctx); err == nil {
        break
    }
    if err := backoff.WaitContext(ctx, backoff.ExponentialWithJitter(base, attempt)); err != nil {
        return err // context cancelled
    }
}
```

Key capabilities:
- Overflow protection: `attempt` is capped at 62 to prevent `1 << attempt` overflow
- Cryptographic randomness (`crypto/rand`) for jitter with two-level fallback — never blocks, never panics
- `WaitContext` uses `time.NewTimer` + `defer timer.Stop()` — no goroutine leak (unlike `time.After`)

### `uncommons/cron` — Cron Expression Parser

```go
sched, err := cron.Parse("*/5 * * * *")
if err != nil {
    return err
}
next, err := sched.Next(time.Now())
```

Key capabilities:
- Standard 5-field format (minute, hour, day-of-month, month, day-of-week)
- Full syntax: `*`, ranges (`1-5`), steps (`*/5`, `1-5/2`), lists (`1,3,5`), combinations
- Cap of 527,040 iterations in `Next()` to prevent infinite loops on contradictory schedules (e.g. "Feb 30")

### `uncommons/errgroup` — Goroutine Group with Panic Recovery

Similar to `golang.org/x/sync/errgroup` but converts panics to errors instead of crashing.

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLogger(logger)

g.Go(func() error { return serviceA.Run(ctx) })
g.Go(func() error { return serviceB.Run(ctx) })

if err := g.Wait(); err != nil {
    // includes recovered panics as ErrPanicRecovered
    return err
}
```

### `uncommons/jwt` — JWT without External Dependencies

```go
// Sign
token, err := jwt.Sign(jwt.MapClaims{"sub": userID, "exp": exp}, jwt.AlgHS256, secret)

// Verify signature only
parsed, err := jwt.Parse(token, secret, []string{jwt.AlgHS256})

// Verify signature + time claims
parsed, err := jwt.ParseAndValidate(token, secret, []string{jwt.AlgHS256})
```

Key capabilities:
- Pure `crypto/stdlib` — no third-party JWT library
- Explicit algorithm allowlist — callers must pass allowed algorithms; prevents algorithm confusion attacks (`none` alg, RSA→HMAC downgrade)
- `Parse` (signature only) vs `ParseAndValidate` (signature + time claims) — explicit contract
- 8192-byte max token length — DoS protection
- `hmac.Equal` for constant-time signature comparison
- `Token.SignatureValid bool` replaces the old `Token.Valid` to clarify scope

### `uncommons/runtime` — Panic Recovery, Safe Goroutines, Error Reporter

```go
// Safe goroutine that logs panics and keeps running
runtime.SafeGoWithContextAndComponent(ctx, logger, "worker", "ProcessQueue", runtime.KeepRunning, func(ctx context.Context) {
    processQueue(ctx)
})

// Or crash the process on panic (for critical paths)
runtime.SafeGo(logger, "critical-worker", runtime.CrashProcess, func() {
    criticalWork()
})
```

Key capabilities:
- `PanicPolicy`: `KeepRunning` vs `CrashProcess`
- `RecordPanicToSpan` — panics are visible in distributed traces
- `InitPanicMetrics` — automatic `panic_recovered_total` OTEL counter
- `ErrorReporter` interface for Sentry/Datadog/etc. integration
- `SetProductionMode` / `IsProductionMode` — global environment flag shared by `assert` and `log`

### `uncommons/safe` — Panic-Free Math, Regex, and Slice Operations

```go
// No panic on division by zero
result, err := safe.Divide(numerator, denominator)
resultOrZero := safe.DivideOrZero(numerator, denominator)

// Cached, non-panicking regex
matched, err := safe.MatchString(`^\d+$`, input)

// Safe slice access
first, err := safe.First(items)
last, err := safe.Last(items)
at, err := safe.At(items, 3)
// or with fallback:
at := safe.AtOrDefault(items, 3, defaultValue)
```

### `uncommons/net/http/ratelimit` — Redis-Backed Rate Limit Storage for Fiber

```go
storage := ratelimit.NewRedisStorage(redisClient,
    ratelimit.WithRedisStorageLogger(logger),
)
// Pass storage to any Fiber rate limiter middleware
```

Implements `fiber.Storage`. Uses `SCAN` for `Reset()` — does not block Redis with `KEYS *`.

---

## 3. Redesigned Packages

### `log` — Interface Redesigned (Breaking)

The most radical change in the library. The old printf-style interface (~15 methods) is replaced by a 5-method interface aligned with `slog` semantics.

```go
// lib-commons: printf-style, no context, no typed fields
logger.Infof("processing transaction %s for org %s", txID, orgID)
logger.Error("failed to connect:", err)

// lib-uncommons: structured, context-aware, typed fields
logger.Log(ctx, log.LevelInfo, "processing transaction",
    log.String("transaction_id", txID),
    log.String("organization_id", orgID),
)
logger.Log(ctx, log.LevelError, "failed to connect", log.Err(err))
```

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Interface | ~15 printf methods | 5 methods: `Log`, `With`, `WithGroup`, `Enabled`, `Sync` |
| Fields | Variadic `any` | Typed `Field`: `String()`, `Int()`, `Bool()`, `Err()`, `Any()` |
| Context | Not in interface | `Log` receives `ctx` as first argument |
| NopLogger | `NoneLogger` struct | `NewNop() Logger` — returns interface, not pointer |
| Log injection | Not addressed | CWE-117 prevention in `GoLogger` (escapes `\n`, `\r`, `\t`, `\x00`) |
| SafeError | Does not exist | In production mode, only logs the error _type_, never the message |
| Level convention | `PanicLevel=0` ... `DebugLevel=5` | `LevelError=0` ... `LevelDebug=3` (lower = more severe) |
| Sanitizer | Does not exist | `SanitizeExternalResponse(statusCode)` — avoids leaking external HTTP details |

**Migration:**
```go
// Before
logger.Infof("user %s logged in", userID)
logger.WithFields("request_id", reqID).Error("failed")

// After
logger.Log(ctx, log.LevelInfo, "user logged in", log.String("user_id", userID))
logger.With(log.String("request_id", reqID)).Log(ctx, log.LevelError, "failed")
```

---

### `opentelemetry` — Redaction, Explicit Types, Clean API

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Bootstrap | `InitializeTelemetry(cfg *TelemetryConfig) *Telemetry` (panics on error) | `NewTelemetry(cfg TelemetryConfig) (*Telemetry, error)` |
| Global providers | Always applied | `ApplyGlobals()` is opt-in |
| Tracer/Meter access | Public struct fields | `(*Telemetry).Tracer(name) (trace.Tracer, error)` |
| Shutdown | `ShutdownTelemetry()` only | `ShutdownTelemetry()` + `ShutdownTelemetryWithContext(ctx) error` |
| Obfuscation | `FieldObfuscator` interface + `DefaultObfuscator` | `Redactor` with `RedactionRule` (FieldPattern, PathPattern, Action) |
| Redaction actions | Mask only (`"********"`) | `RedactionMask`, `RedactionHash` (per-instance HMAC-SHA256), `RedactionDrop` |
| Span processor | `AttrBagSpanProcessor` | Adds `RedactingAttrBagSpanProcessor` — redacts sensitive attributes in-flight |
| Span attributes | `SetSpanAttributesFromStruct` (deprecated) | `SetSpanAttributesFromValue` + `BuildAttributesFromValue` with depth cap (32) and attr cap (128) |

**Migration:**
```go
// Before
tl := opentelemetry.InitializeTelemetry(&opentelemetry.TelemetryConfig{...})

// After
tl, err := opentelemetry.NewTelemetry(opentelemetry.TelemetryConfig{...})
if err != nil {
    return err
}
tl.ApplyGlobals() // opt-in
```

The most important change in telemetry: `FieldObfuscator` is removed and replaced by `Redactor` with composable rules. `RedactionHash` uses an HMAC key generated via `crypto/rand` per `Redactor` instance — prevents rainbow table attacks on low-entropy PII like email addresses.

---

### `otel/metrics` — Error Returns, NopFactory, No Positional Org/Ledger Args

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Factory construction | `NewMetricsFactory(meter, logger) *MetricsFactory` (panics on error) | Returns `(*MetricsFactory, error)` |
| Nop factory | Does not exist | `NewNopFactory() *MetricsFactory` — for tests and disabled metrics |
| Domain recorders | `RecordAccountCreated(ctx, orgID, ledgerID, ...)` — positional args | No positional org/ledger — use `...attribute.KeyValue` |
| Builders | Mutable | Immutable — each `WithLabels`/`WithAttributes` returns a new builder snapshot |

---

### `circuitbreaker` — Validation, Metrics, Safe Health Checker

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| `GetOrCreate` | Returns `CircuitBreaker` (no error) | Returns `(CircuitBreaker, error)` |
| Config validation | `NewHealthChecker` panics on invalid interval | `NewHealthCheckerWithValidation` returns error |
| Metrics | Does not exist | `WithMetricsFactory(f)` option |
| `IsHealthy` | Does not exist | `IsHealthy(serviceName) bool` — only `Closed` state is healthy |
| `Execute` on Manager | Does not exist | `(Manager).Execute(serviceName, fn)` — direct execution without `GetOrCreate` |

---

### `redis` — Declarative Topology, GCP IAM, Typed Lock API

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Config | Flat struct with `Mode string` | `Topology` struct with `Standalone`, `Sentinel`, `Cluster` sub-structs — exactly-one pattern |
| Auth | `Password string` and `UseGCPIAMAuth bool` mixed in config | `Auth` struct with `StaticPassword *StaticPasswordAuth` or `GCPIAM *GCPIAMAuth` |
| Credential redaction | `Password` in plain text on the struct | `StaticPasswordAuth.String()` and `GCPIAMAuth.String()` redact — safe for accidental `%v` |
| Package-level logger | Does not exist | `SetPackageLogger(l log.Logger)` via `atomic.Value` — for nil-receiver diagnostics |
| `Status()` | Does not exist | Returns `Status{Connected, LastRefreshError, LastRefreshAt, RefreshLoopRunning}` |
| Lock API | `DistributedLocker` with `*redsync.Mutex` exposed | `LockManager` with `LockHandle` interface — mutex is not exposed |
| `TryLock` return | `(*redsync.Mutex, bool, error)` | `(LockHandle, bool, error)` — distinguishes contention from real errors |
| Lock options validation | Not validated | Strict: max 1000 tries, drift factor in [0,1), expiry must be positive |

---

### `postgres` — SanitizedError, DSN Validation, Migration Safety

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Connect API | `Connect() error` | `Connect(ctx) error` + `Resolver(ctx)` for lazy reconnect |
| DB access | `GetDB() (dbresolver.DB, error)` | `Resolver(ctx)` (lazy reconnect) + `Primary() (*sql.DB, error)` |
| Migration config | Embedded in `PostgresConnection` | Separate `MigrationConfig` struct + `NewMigrator(cfg)` |
| Error sanitization | DB errors may contain DSN | `SanitizedError` — `Unwrap()` deliberately returns `nil` to break the error chain and prevent credential leaks |
| DSN validation | Not validated | Validates both URL and `key=value` formats; warns on `sslmode=disable` |
| Path traversal | Not protected | `sanitizePath` rejects `..` components |

**The `SanitizedError.Unwrap() nil` pattern is intentional:** `errors.Is` cannot traverse into the original error, which may contain DSN credentials in its message. This is documented behavior, not a bug.

---

### `mongo` — Backoff Rate-Limiting, TLS, URI Builder

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| API | `MongoConnection` with `GetDB()` | `Client` with `Client()` and `ResolveClient()` (lazy + backoff) |
| Reconnect | Attempts without limiting | `ResolveClient` tracks `connectAttempts`, applies exponential backoff capped at 30s |
| TLS | Not supported | `TLSConfig` with base64 CA cert, configurable min version (default TLS 1.2) |
| URI builder | Does not exist | `BuildURI(URIConfig) (string, error)` with 8 distinct error types |
| Credential clearing | Does not exist | `c.cfg.URI = ""` after connect — credentials do not linger in memory |
| `EnsureIndexes` | Fails on first error | Collects all errors via `errors.Join` |

---

### `server` — Expanded API, Hooks, `ServersStarted` Channel

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| `GracefulShutdown` | Exists | Removed (breaking) |
| `ServerManager` | `WithHTTPServer`, `WithGRPCServer`, `StartWithGracefulShutdown` | Adds `WithShutdownChannel`, `WithShutdownTimeout`, `WithShutdownHook` |
| Error return | `StartWithGracefulShutdown()` void | `StartWithGracefulShutdownWithError() error` — new |
| Test coordination | Does not exist | `ServersStarted() <-chan struct{}` — signals when servers are listening |

---

### `net/http` — Unified API, SSRF, Cursor Types, Ownership Verification

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Response helpers | ~15 individual functions (`Unauthorized`, `BadRequest`, `Created`, ...) | Removed; replaced by `Respond`, `RespondStatus`, `RespondError`, `RenderError` |
| `Ping` | Returns `"healthy"` | Returns `"pong"` |
| `ErrorResponse.Code` | `string` | `int` |
| SSRF proxy | No protection | `ReverseProxyPolicy` with scheme/host allowlist, private IP blocking, no redirect following |
| Cursor pagination | Single `Cursor{ID, PointsNext}` | `TimestampCursor`, `SortCursor`, encode/decode functions |
| Ownership verification | Does not exist | `ParseAndVerifyTenantScopedID`, `ParseAndVerifyResourceScopedID` with `TenantOwnershipVerifier` and `ResourceOwnershipVerifier` func types |
| Custom validation tags | Does not exist | `positive_decimal`, `positive_amount`, `nonnegative_amount` registered on the validator |
| CORS | Reads env vars implicitly | `CORSOption` functional options: `WithCORSLogger`, etc. |

**Migration — response helpers:**
```go
// Before
return http.OK(c, payload)
return http.BadRequest(c, payload)
return http.NotFound(c, "0042", "Account Not Found", "account does not exist")

// After
return http.Respond(c, fiber.StatusOK, payload)
return http.Respond(c, fiber.StatusBadRequest, payload)
return http.RespondError(c, fiber.StatusNotFound, "Account Not Found", "account does not exist")
```

---

### `rabbitmq` — Context-Aware, TLS Safety, Credential Sanitization

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| API | No `Context` variants | All methods have `*Context(ctx)` variants |
| TLS | No validation | `AllowInsecureTLS bool` — explicit flag; returns `ErrInsecureTLS` if not set |
| Health check URL validation | Not validated | Validates scheme, host, and rejects embedded credentials |
| Credential sanitization | Errors may contain password | `sanitizeAMQPErr` uses `url.URL.Redacted()` before logging |
| IPv6 | Not supported | `BuildRabbitMQConnectionString` brackets IPv6 addresses |

---

### `crypto` — Nil Safety, Credential Redaction

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| Nil receiver | Panics | All methods check `c == nil` first |
| `String()`/`GoString()` | Not implemented | Implemented — returns `"Crypto{keys:REDACTED}"` |

---

### `license` — No Panic, More Termination Options

| Aspect | lib-commons | lib-uncommons |
|---|---|---|
| `DefaultHandler` | Panics | Records assertion failure, no panic |
| `New()` | No options | `New(opts ...ManagerOption)` with `WithLogger(l)` |
| `TerminateWithError` | Does not exist | Returns error without invoking the handler |
| `TerminateSafe` | Does not exist | Invokes handler + returns error if uninitialized |

---

### `constants` — New Groups, Utility Function

| Addition | Detail |
|---|---|
| `SanitizeMetricLabel(value) string` | Truncates OTEL labels to 64 chars — `MaxMetricLabelLength = 64` |
| `CodeMetadataKeyLengthExceeded` / `CodeMetadataValueLengthExceeded` | New error codes (0050, 0051) |
| `CodeOnHoldExternalAccount` (0098) | New |
| `AttrPrefixAppRequest`, `AttrPrefixAssertion`, `AttrPrefixPanic` | Standardized OTEL attribute prefixes |
| `MetricPanicRecoveredTotal`, `MetricAssertionFailedTotal` | Metric names as constants |
| `EventAssertionFailed`, `EventPanicRecovered` | OTEL event names as constants |
| `RateLimitLimit`, `RateLimitRemaining`, `RateLimitReset` | Standardized rate limit headers |
| `NOTED` | New transaction status |

---

## 4. Cross-Cutting Patterns

### Error Handling — From Silent Failure to Explicit Failure

`lib-commons` used `panic` in bootstrap paths (`InitializeTelemetry`, `NewHealthChecker`, `NewMetricsFactory`). `lib-uncommons` converts all of them to `(T, error)`:

- Tests can now test failure scenarios
- Services can do graceful degradation instead of crashing
- Errors are handled at the layer that has context to decide what to do

### Nil Safety — Systematic Across the Entire Library

In `lib-commons`, nil receivers caused panics. In `lib-uncommons`:
- All primary methods check `if receiver == nil` before operating
- `NewNop()`, `NewNopFactory()` provide no-ops that satisfy interfaces
- Context resolver helpers (`resolveTracer`, `resolveMetricFactory`, `resolveLogger`) return no-ops, never nil

### Credential Redaction — A First-Class Security Feature

| Location | Protection added |
|---|---|
| `Crypto.String()`/`GoString()` | Redacts keys |
| `StaticPasswordAuth.String()` | Redacts password |
| `GCPIAMAuth.String()` | Redacts GCP credentials |
| `SanitizedError.Unwrap() nil` | Breaks error chain to prevent DSN leakage |
| `sanitizeAMQPErr` | Uses `url.URL.Redacted()` |
| `mongo: c.cfg.URI = ""` | Zeroes URI after connect |
| `log.SafeError` | In production, logs only the error _type_, never the message |

### Concurrency Safety — Explicit Patterns

- Redis `SetPackageLogger` uses `atomic.Value` (lock-free)
- Circuit breaker `GetOrCreate` uses correct double-checked locking
- Metrics factory uses `sync.Map` with `LoadOrStore`
- Redis/Mongo reconnect has backoff with rate-limiting via `connectAttempts`
- `Asserter` uses `sync.RWMutex` for the license handler

---

## 5. Intentional Breaking Changes (Removals)

| Removed | Replacement |
|---|---|
| `GracefulShutdown` struct | `ServerManager` with `WithShutdownHook`, `WithShutdownChannel` |
| `FieldObfuscator` interface | `Redactor` with `RedactionRule` |
| `InitializeTelemetry` | `NewTelemetry` (returns error) |
| `NewHealthChecker` (panics) | `NewHealthCheckerWithValidation` (returns error) |
| `GetDB()` (mongo) | `Client()` and `ResolveClient()` |
| `GetDB()` (postgres) | `Resolver(ctx)` |
| 15 individual HTTP response functions | `Respond`, `RespondStatus`, `RespondError`, `RenderError` |
| `WithOrganizationLabels`/`WithLedgerLabels` (metrics) | Use `...attribute.KeyValue` directly |
| Positional `orgID`, `ledgerID` in domain recorders | Use `...attribute.KeyValue` directly |
| `HealthSimple` alias | Use `Ping` directly |
| Redis key builders (`GenericInternalKey`, etc.) | Moved to application domain |
| Domain validations (`ValidateCountryAddress`, `ValidateAccountType`, etc.) | Moved to Midaz application |
| Printf-style log methods (`Info`, `Infof`, `Fatal`, ...) | 5-method `Logger` interface |
| `StringToInt` (no error return) | `StringToIntOrDefault` |
| `GenerateUUIDv7()` without error return | Returns `(uuid.UUID, error)` |
| `Token.Valid` (jwt) | `Token.SignatureValid bool` — clarifies scope |

---

## 6. Design Principles Summary

`lib-uncommons` is a rewrite guided by three principles that were absent or inconsistent in `lib-commons`:

**1. Explicit failures**  
Everything that can fail returns `error`. Zero panics in production code — including bootstrap, where they were previously acceptable. This makes failure scenarios testable and gives the calling layer the power to decide how to recover.

**2. Security by default**  
Credentials are redacted in `String()`, DSNs are zeroed after connection, error chains are deliberately broken to avoid leaking secrets, and PII in spans is hashed with a per-instance HMAC key. These are not opt-in features — they are the default behavior.

**3. Integrated observability**  
Assertions, panics, and circuit breaker state changes emit OTEL metrics and span events automatically — without application code needing to instrument them explicitly. Observability is infrastructure, not the caller's responsibility.

The new packages (`assert`, `backoff`, `cron`, `errgroup`, `jwt`, `runtime`, `safe`) close gaps that previously forced each service to re-implement the same patterns — with inconsistent results and varying levels of security.
