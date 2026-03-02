# Multi-Tenant Migration Plan: `lib-commons/v3` -> `lib-uncommons/v2`

## Scope and baseline

- [UPDATED] Source analyzed from real code, not mirror docs: `~/code/lerian/lib-commons/commons/tenant-manager/**`.
- [UPDATED] Current tenant-manager size (actual):
  - Go files: **26** total
  - Source (non-test): **4,761 LOC**
  - Tests: **8,899 LOC**
- [UPDATED] Package/file coverage validated:
  - `client` (2), `consumer` (3), `core` (6), `middleware` (4), `mongo` (2), `postgres` (3), `rabbitmq` (2), `s3` (2), `valkey` (1), `internal/testutil` (1).
- Migration objective remains: move tenant-manager into `github.com/LerianStudio/lib-uncommons/v2` with **no dependency on `github.com/LerianStudio/lib-commons/v3`**.

---

## 1) Dependency analysis (`go.mod` impact)

### 1.1 Direct external dependencies used by tenant-manager code

- `github.com/bxcodec/dbresolver/v2`
- `github.com/gofiber/fiber/v2`
- `github.com/golang-jwt/jwt/v5`
- `github.com/jackc/pgx/v5/stdlib`
- `github.com/rabbitmq/amqp091-go`
- `github.com/redis/go-redis/v9`
- `go.mongodb.org/mongo-driver/mongo`
- `go.opentelemetry.io/otel/trace`
- test-only: `github.com/alicebob/miniredis/v2`, `github.com/stretchr/testify/*`, `go.uber.org/goleak`

### 1.2 Verification against both `go.mod` files

- [UPDATED] Verified in `lib-commons/go.mod`: all dependencies above are present.
- [UPDATED] Verified in `lib-uncommons/go.mod`: present already: dbresolver, fiber, pgx, rabbitmq, redis, mongo-driver, otel, miniredis, testify.
- [UPDATED] Missing in `lib-uncommons/go.mod` and required for tenant-manager migration:
  - `github.com/golang-jwt/jwt/v5` (runtime)
  - `go.uber.org/goleak` (tests)

No `lib-commons` dependency must be introduced.

---

## 2) Target package structure in `lib-uncommons`

Create top-level multi-tenant area under `uncommons/tenantmanager`:

- `uncommons/tenantmanager/core`
- `uncommons/tenantmanager/client`
- `uncommons/tenantmanager/postgres`
- `uncommons/tenantmanager/mongo`
- `uncommons/tenantmanager/rabbitmq`
- `uncommons/tenantmanager/consumer`
- `uncommons/tenantmanager/middleware`
- `uncommons/tenantmanager/s3`
- `uncommons/tenantmanager/valkey`
- `uncommons/tenantmanager/internal/testutil` (tests/shared test helpers)

Rationale:

- keeps migrated feature cohesive
- avoids leaking tenant-manager internals into existing shared connectors
- minimizes import churn for consumers

---

## 3) TenantID context key unification strategy (validated)

### Current implementations (actual code)

- `lib-commons tenant-manager core`: private key `tenantID` in `core/context.go`.
- `lib-uncommons outbox`: exported key `outbox.tenant_id` in `uncommons/outbox/tenant.go`.

These are not interoperable today because keys and key types differ.

### Validated strategy

- [UPDATED] Canonical tenant context API should live in `uncommons/tenantmanager/core/context.go`:
  - `ContextWithTenantID(ctx, tenantID)`
  - `GetTenantIDFromContext(ctx)`
- [UPDATED] To keep backward compatibility with outbox behavior, outbox helpers must support **dual-read**:
  - first read canonical key
  - then fallback to `outbox.TenantIDContextKey`
- [UPDATED] Outbox `ContextWithTenantID` should write canonical key and may also write legacy key during transition.
- [UPDATED] Preserve `outbox.TenantIDContextKey` export and document as deprecated.

### Compatibility caveats that must be handled

- [UPDATED] `outbox.ContextWithTenantID` currently tolerates `nil` context and trims spaces.
- [UPDATED] `tenant-manager/core.ContextWithTenantID` currently does not normalize/trim.
- [UPDATED] Canonical implementation should normalize to trimmed non-empty IDs and handle `nil` contexts consistently, or outbox wrappers should preserve current semantics.

### Required interoperability tests

- set tenant in core -> read in outbox
- set tenant in outbox -> read in core
- set tenant via direct legacy key -> outbox accessor still works
- nil context + whitespace tenant ID behavior parity tests

---

## 4) Import path mapping and real API compatibility

### Package path mapping (old -> new)

- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/core` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/core`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/client` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/client`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/postgres` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/postgres`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/mongo` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/mongo`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/rabbitmq` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/rabbitmq`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/consumer` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/consumer`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/middleware` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/middleware`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/s3` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/s3`
- `github.com/LerianStudio/lib-commons/v3/commons/tenant-manager/valkey` -> `github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/valkey`

### Internal dependency remaps and compatibility status

- `commons` -> `uncommons`: [UPDATED] **partially compatible** (`NewTrackingFromContext` exists; logger type changed).
- `commons/log` -> `uncommons/log`: [UPDATED] **not API-compatible** (interface changed completely).
- `commons/opentelemetry` -> `uncommons/opentelemetry`: [UPDATED] **not API-compatible** for several helper signatures.
- `commons/postgres` -> `uncommons/postgres`: [UPDATED] **not API-compatible** (`PostgresConnection` type absent in v2).
- `commons/mongo` -> `uncommons/mongo`: [UPDATED] **not API-compatible** (`MongoConnection` type absent in v2).
- `commons/net/http` -> `uncommons/net/http`: [UPDATED] **compatible for current tenant-manager usage** (`ExtractTokenFromHeader` matches expected behavior/signature).

---

## 5) Concrete function/type mismatches requiring adaptation

The major migration effort is adaptation, not import rewriting.

### 5.1 Logging API mismatch (`commons/log.Logger` -> `uncommons/log.Logger`)

Current tenant-manager uses old logger methods that do not exist in v2:

- [UPDATED] `Infof`, `Warnf`, `Errorf`, `Info`, `Warn`, `Error`, `Debug`, `WithFields`, `Sync()`
- [UPDATED] old fallback type `&libLog.NoneLogger{}` (used in `consumer.NewMultiTenantConsumer`) does not exist in v2.

Required migration shape:

- replace with `logger.Log(ctx, level, msg, fields...)`
- use `log.String`, `log.Int`, `log.Bool`, `log.Any`, `log.Err`
- replace `WithFields(...)` with `With(...)`
- replace `NoneLogger` fallback with `log.NewNop()`

### 5.2 OpenTelemetry helper signature mismatches

Current calls in tenant-manager use old pointer-based APIs:

- [UPDATED] `ExtractHTTPContext(c *fiber.Ctx)` -> now `ExtractHTTPContext(ctx context.Context, c *fiber.Ctx)`
- [UPDATED] `InjectHTTPContext(headers *http.Header, ctx context.Context)` -> now `InjectHTTPContext(ctx context.Context, headers http.Header)`
- [UPDATED] `HandleSpanError(span *trace.Span, ...)` -> now `HandleSpanError(span trace.Span, ...)`
- [UPDATED] `HandleSpanBusinessErrorEvent(span *trace.Span, ...)` -> now `HandleSpanBusinessErrorEvent(span trace.Span, ...)`
- [UPDATED] `HandleSpanEvent(span *trace.Span, ...)` -> now `HandleSpanEvent(span trace.Span, ...)`

### 5.3 Postgres connector type mismatch

Tenant-manager currently depends on `commons/postgres.PostgresConnection` concrete shape:

- [UPDATED] direct struct construction with fields:
  - `ConnectionStringPrimary`, `ConnectionStringReplica`, `PrimaryDBName`, `ReplicaDBName`
  - `MaxOpenConnections`, `MaxIdleConnections`, `SkipMigrations`, `Logger`
  - reads/writes `ConnectionDB *dbresolver.DB`
- [UPDATED] method contracts used:
  - `Connect() error`
  - `GetDB() (dbresolver.DB, error)`

`uncommons/postgres` v2 exposes:

- `New(cfg Config) (*Client, error)`
- `Connect(ctx) error`
- `Resolver(ctx) (dbresolver.DB, error)`
- `Primary() (*sql.DB, error)`
- `Close() error`

Required adaptation:

- introduce tenant-manager-local adapter wrapping `*uncommons/postgres.Client` and exposing legacy manager-facing behavior (`GetConnection`, `GetDB`, settings update flow, close semantics).

### 5.4 Mongo connector type mismatch

Tenant-manager currently depends on `commons/mongo.MongoConnection` concrete shape:

- [UPDATED] direct struct construction with fields `ConnectionStringSource`, `Database`, `Logger`, `MaxPoolSize`, `DB`
- [UPDATED] method contracts used: `Connect(ctx)`, `GetDB(ctx)`, `GetDatabaseName()`

`uncommons/mongo` v2 exposes:

- `NewClient(ctx, cfg, opts...) (*Client, error)`
- `Client(ctx)`, `ResolveClient(ctx)`, `Database(ctx)`, `DatabaseName() (string, error)`
- `Close(ctx)`, `EnsureIndexes(ctx, collection, indexes...)`

Required adaptation:

- per-tenant wrapper over `*uncommons/mongo.Client`
- keep tenant-manager manager contract stable (`GetConnection`, `GetDatabaseForTenant`, close/eviction behavior)

### 5.5 Runtime behavior mismatch in constructor

- [UPDATED] `consumer.NewMultiTenantConsumer(...)` currently panics when `rabbitmq` or `redisClient` is nil.
- [UPDATED] In `lib-uncommons` policy, production paths should return explicit errors, not panic.

Required adaptation:

- add error-returning constructor path (recommended: `NewMultiTenantConsumer(...) (*MultiTenantConsumer, error)`), with optional deprecated panic wrapper only if needed for compatibility.

---

## 6) Consumer migration impact (measured)

### 6.1 Exact import-file counts for requested consumer repos

- [UPDATED] `~/code/lerian/midaz`: **79** files import tenant-manager (`36` non-test, `43` test)
- [UPDATED] `~/code/lerian/reporter`: **21** files import tenant-manager (`9` non-test, `12` test)
- [UPDATED] `~/code/lerian/matcher`: **0**
- [UPDATED] `~/code/lerian/fetcher`: **0**
- [UPDATED] `~/code/lerian/tracer`: **0**

### 6.2 Any other repos under `~/code/lerian` importing tenant-manager

- [UPDATED] Yes, only:
  - `lib-commons` (tenant-manager source itself): **19** files importing tenant-manager packages internally
  - `midaz`: **79**
  - `reporter`: **21**
- [UPDATED] No additional repos detected beyond those.

### 6.3 Rollout priority correction

- [UPDATED] Priority should focus on `midaz` and `reporter` only.
- [UPDATED] `matcher`, `fetcher`, `tracer` can be removed from migration execution scope unless future imports are introduced.

---

## 7) Breaking-change management and compatibility strategy

Tier A (preserve where feasible):

- package names and major tenant-manager exported symbols
- core error sentinels (`ErrTenantNotFound`, `ErrServiceNotConfigured`, etc.)
- middleware JSON envelope shape unless explicitly revised

Tier B (controlled/documented changes):

- [UPDATED] logger internals and test logger implementations must move to v2 structured API
- [UPDATED] opentelemetry helper call signatures must be updated
- [UPDATED] constructor panic behavior may move to explicit errors

Transition mechanics:

- update `MIGRATION_MAP.md` with symbol mapping
- update README examples for new namespace
- add deprecation notes for any temporary compatibility wrappers

---

## 8) Updated execution order

1. [UPDATED] Add missing deps to `lib-uncommons/go.mod`: `jwt/v5`, `goleak`.
2. Create `uncommons/tenantmanager/*` package skeletons.
3. Migrate low-coupling packages first: `core`, `s3`, `valkey`.
4. Migrate `client` and fix opentelemetry helper signatures (`InjectHTTPContext`, span helpers).
5. Migrate `postgres` manager with adapter over `uncommons/postgres.Client`.
6. Migrate `mongo` manager with adapter over `uncommons/mongo.Client`.
7. Migrate `rabbitmq` manager (mostly independent of uncommons connector APIs).
8. Migrate middleware (`tenant`, `multi_pool`) with logger + otel + context API updates.
9. Migrate consumer and replace panic constructor path with error-returning path.
10. Implement TenantID context-key unification with dual-read compatibility in outbox.
11. Migrate and fix all tests (including testutil logger rewrites + goleak checks).
12. Consumer rollout: `midaz` pilot first, then `reporter`.

---

## 9) Updated effort estimate

- [UPDATED] Tenant-manager migration inside `lib-uncommons`: **5-7 dev days**
  - heavy areas: logger conversion, postgres/mongo adapter layers, otel signature updates
- [UPDATED] Midaz migration: **3-5 dev days**
- [UPDATED] Reporter migration: **1-2 dev days**
- [UPDATED] Cross-repo validation/hardening: **1-2 dev days**

- [UPDATED] Total revised program estimate: **10-16 dev days**
  - lower bound assumes parallel work and clean integration environments
  - upper bound covers adaptation/test churn in postgres/mongo and middleware paths

---

## 10) Acceptance criteria (refined)

- all tenant-manager packages compile in `lib-uncommons/v2` with zero `lib-commons` imports
- concrete API mismatches above are resolved (logger, otel signatures, postgres/mongo adapters)
- TenantID context interoperability passes bidirectional tests with outbox
- full migrated tenant-manager tests pass (including goleak/race-sensitive areas)
- `midaz` and `reporter` compile and run with updated imports
- migration docs are updated (`MIGRATION_MAP.md`, README, this plan)
