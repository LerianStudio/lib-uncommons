# Vindo da lib-commons

Este guia é para engenheiros migrando da `lib-commons` para a `lib-uncommons`.  
Cobre todas as diferenças relevantes: novos pacotes, APIs redesenhadas, breaking changes e os princípios de design por trás de cada decisão.

> **English version:** [COMING_FROM_COMMONS_en.md](COMING_FROM_COMMONS_en.md)

---

## Índice

1. [Módulo e Namespace](#1-módulo-e-namespace)
2. [Novos Pacotes](#2-novos-pacotes)
3. [Pacotes Redesenhados](#3-pacotes-redesenhados)
   - [log](#log--interface-redesenhada-breaking)
   - [opentelemetry](#opentelemetry--redação-tipos-explícitos-api-limpa)
   - [opentelemetry/metrics](#otelmetrics--error-returns-nopfactory-sem-positional-args-de-org-ledger)
   - [circuitbreaker](#circuitbreaker--validação-métricas-health-checker-seguro)
   - [redis](#redis--topologia-declarativa-gcp-iam-lock-api-tipada)
   - [postgres](#postgres--sanitizederror-validação-de-dsn-segurança-de-migrations)
   - [mongo](#mongo--backoff-rate-limiting-tls-uri-builder)
   - [server](#server--api-expandida-hooks-canal-serversstarted)
   - [net/http](#nethttp--api-unificada-ssrf-tipos-de-cursor-verificação-de-ownership)
   - [rabbitmq](#rabbitmq--context-aware-segurança-tls-sanitização-de-credenciais)
   - [crypto](#crypto--nil-safety-redação-de-credenciais)
   - [license](#license--sem-panic-mais-opções-de-terminação)
   - [constants](#constants--novos-grupos-função-utilitária)
4. [Padrões Transversais](#4-padrões-transversais)
5. [Breaking Changes Intencionais (Remoções)](#5-breaking-changes-intencionais-remoções)
6. [Resumo dos Princípios de Design](#6-resumo-dos-princípios-de-design)

---

## 1. Módulo e Namespace

| | lib-commons | lib-uncommons |
|---|---|---|
| Module path | `github.com/LerianStudio/lib-commons` | `github.com/LerianStudio/lib-uncommons/v2` |
| Prefixo de pacote | `commons/` | `uncommons/` |
| Versão do Go | 1.23.x | 1.25.7 |
| Versionamento explícito | nenhum | `/v2` no module path |

O sufixo `/v2` no module path sinaliza que a `lib-uncommons` gerencia breaking changes explicitamente e permite que consumidores rodem ambas as bibliotecas em paralelo durante a migração.

---

## 2. Novos Pacotes

Estes pacotes **não têm equivalente na lib-commons**. Eles cobrem lacunas que antes forçavam cada serviço a reimplementar os mesmos padrões do zero — acumulando inconsistências e potenciais bugs de segurança.

### `uncommons/assert` — Assertions de Produção com Telemetria

Substitui guards ad-hoc `if err != nil { panic(...) }` por assertions estruturadas e observáveis que nunca crasham o processo.

```go
// lib-commons: sem equivalente — cada serviço escrevia seus próprios guards
if amount.IsZero() {
    panic("amount cannot be zero")
}

// lib-uncommons
a := assert.New(ctx, logger, "payment-service", "ProcessPayment")
if err := a.That(ctx, assert.ValidAmount(amount), "amount deve ser positivo"); err != nil {
    return err
}
```

Principais capacidades:
- Métodos: `That`, `NotNil`, `NotEmpty`, `NoError`, `Never`, `Halt`
- Cada falha automaticamente: incrementa o counter OTEL `assertion_failed_total`, adiciona um span event, chama `span.RecordError()` e seta o status do span para Error
- Stack traces são suprimidos quando `IsProductionMode()` retorna true — previne vazamento de dados sensíveis em logs
- `Halt(err)` usa `runtime.Goexit()` para sair apenas da goroutine atual sem crashar o processo
- **Biblioteca de predicados de domínio** (`predicates.go`): ~20 predicados tipados para domínio financeiro — `DebitsEqualCredits`, `TransactionCanTransitionTo`, `BalanceSufficientForRelease`, `ValidAmount`, `ValidUUID`, entre outros

### `uncommons/backoff` — Backoff Exponencial com Full Jitter

Anteriormente a lógica de backoff estava embutida dentro de cada connector individualmente e não era reutilizável.

```go
// lib-uncommons — autônomo e reutilizável
for attempt := 0; attempt < maxAttempts; attempt++ {
    if err := doSomething(ctx); err == nil {
        break
    }
    if err := backoff.WaitContext(ctx, backoff.ExponentialWithJitter(base, attempt)); err != nil {
        return err // contexto cancelado
    }
}
```

Principais capacidades:
- Proteção contra overflow: `attempt` é limitado a 62 para prevenir overflow de `1 << attempt`
- Aleatoriedade criptográfica (`crypto/rand`) para jitter com fallback em dois níveis — nunca bloqueia, nunca panics
- `WaitContext` usa `time.NewTimer` + `defer timer.Stop()` — sem goroutine leak (ao contrário de `time.After`)

### `uncommons/cron` — Parser de Expressões Cron

```go
sched, err := cron.Parse("*/5 * * * *")
if err != nil {
    return err
}
next, err := sched.Next(time.Now())
```

Principais capacidades:
- Formato padrão de 5 campos (minuto, hora, dia-do-mês, mês, dia-da-semana)
- Sintaxe completa: `*`, ranges (`1-5`), steps (`*/5`, `1-5/2`), listas (`1,3,5`), combinações
- Cap de 527.040 iterações em `Next()` para prevenir loop infinito em schedules contraditórios (ex.: "30 de fevereiro")

### `uncommons/errgroup` — Goroutine Group com Panic Recovery

Similar a `golang.org/x/sync/errgroup`, mas converte panics em errors ao invés de crashar.

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLogger(logger)

g.Go(func() error { return serviceA.Run(ctx) })
g.Go(func() error { return serviceB.Run(ctx) })

if err := g.Wait(); err != nil {
    // inclui panics recuperados como ErrPanicRecovered
    return err
}
```

### `uncommons/jwt` — JWT sem Dependências Externas

```go
// Assinar
token, err := jwt.Sign(jwt.MapClaims{"sub": userID, "exp": exp}, jwt.AlgHS256, secret)

// Verificar apenas assinatura
parsed, err := jwt.Parse(token, secret, []string{jwt.AlgHS256})

// Verificar assinatura + claims temporais
parsed, err := jwt.ParseAndValidate(token, secret, []string{jwt.AlgHS256})
```

Principais capacidades:
- Apenas `crypto/stdlib` — sem biblioteca de JWT de terceiros
- Allowlist explícita de algoritmos — callers devem passar os algoritmos permitidos; previne algorithm confusion attacks (alg `none`, downgrade RSA→HMAC)
- `Parse` (só assinatura) vs `ParseAndValidate` (assinatura + claims temporais) — contrato explícito
- Limite de 8192 bytes para o token — proteção contra DoS
- `hmac.Equal` para comparação de assinatura em tempo constante
- `Token.SignatureValid bool` substitui o antigo `Token.Valid` para deixar o escopo claro

### `uncommons/runtime` — Panic Recovery, Safe Goroutines, Error Reporter

```go
// Goroutine segura que loga panics e continua rodando
runtime.SafeGoWithContextAndComponent(ctx, logger, "worker", "ProcessQueue", runtime.KeepRunning, func(ctx context.Context) {
    processQueue(ctx)
})

// Ou crasha o processo no panic (para caminhos críticos)
runtime.SafeGo(logger, "critical-worker", runtime.CrashProcess, func() {
    criticalWork()
})
```

Principais capacidades:
- `PanicPolicy`: `KeepRunning` vs `CrashProcess`
- `RecordPanicToSpan` — panics ficam visíveis no trace distribuído
- `InitPanicMetrics` — counter OTEL `panic_recovered_total` automático
- Interface `ErrorReporter` para integração com Sentry/Datadog/etc.
- `SetProductionMode` / `IsProductionMode` — flag global de ambiente compartilhada com `assert` e `log`

### `uncommons/safe` — Operações Matemáticas, Regex e de Slice Sem Panic

```go
// Sem panic em divisão por zero
result, err := safe.Divide(numerator, denominator)
resultOrZero := safe.DivideOrZero(numerator, denominator)

// Regex com cache, sem panic
matched, err := safe.MatchString(`^\d+$`, input)

// Acesso seguro a slices
first, err := safe.First(items)
last, err := safe.Last(items)
at, err := safe.At(items, 3)
// ou com fallback:
at := safe.AtOrDefault(items, 3, defaultValue)
```

### `uncommons/net/http/ratelimit` — Rate Limit Storage Redis para Fiber

```go
storage := ratelimit.NewRedisStorage(redisClient,
    ratelimit.WithRedisStorageLogger(logger),
)
// Passa o storage para qualquer middleware de rate limiting do Fiber
```

Implementa a interface `fiber.Storage`. Usa `SCAN` no `Reset()` — não bloqueia o Redis com `KEYS *`.

---

## 3. Pacotes Redesenhados

### `log` — Interface Redesenhada (Breaking)

A mudança mais radical da biblioteca. A antiga interface estilo printf (~15 métodos) é substituída por uma interface de 5 métodos alinhada com a semântica do `slog`.

```go
// lib-commons: estilo printf, sem contexto, sem campos tipados
logger.Infof("processando transação %s para org %s", txID, orgID)
logger.Error("falha ao conectar:", err)

// lib-uncommons: estruturado, com contexto, campos tipados
logger.Log(ctx, log.LevelInfo, "processando transação",
    log.String("transaction_id", txID),
    log.String("organization_id", orgID),
)
logger.Log(ctx, log.LevelError, "falha ao conectar", log.Err(err))
```

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| Interface | ~15 métodos printf | 5 métodos: `Log`, `With`, `WithGroup`, `Enabled`, `Sync` |
| Campos | Variadic `any` | `Field` tipados: `String()`, `Int()`, `Bool()`, `Err()`, `Any()` |
| Contexto | Não está na interface | `Log` recebe `ctx` como primeiro argumento |
| NopLogger | Struct `NoneLogger` | `NewNop() Logger` — retorna interface, não pointer |
| Log injection | Não tratado | Prevenção CWE-117 no `GoLogger` (escapa `\n`, `\r`, `\t`, `\x00`) |
| SafeError | Não existe | Em modo produção, loga apenas o _tipo_ do erro, nunca a mensagem |
| Convenção de nível | `PanicLevel=0` ... `DebugLevel=5` | `LevelError=0` ... `LevelDebug=3` (menor número = mais severo) |
| Sanitizador | Não existe | `SanitizeExternalResponse(statusCode)` — evita vazar detalhes de HTTP externo |

**Migração:**
```go
// Antes
logger.Infof("usuário %s fez login", userID)
logger.WithFields("request_id", reqID).Error("falhou")

// Depois
logger.Log(ctx, log.LevelInfo, "usuário fez login", log.String("user_id", userID))
logger.With(log.String("request_id", reqID)).Log(ctx, log.LevelError, "falhou")
```

---

### `opentelemetry` — Redação, Tipos Explícitos, API Limpa

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| Bootstrap | `InitializeTelemetry(cfg *TelemetryConfig) *Telemetry` (panics em erro) | `NewTelemetry(cfg TelemetryConfig) (*Telemetry, error)` |
| Providers globais | Sempre aplicados | `ApplyGlobals()` é opt-in |
| Acesso a Tracer/Meter | Campos públicos do struct | `(*Telemetry).Tracer(name) (trace.Tracer, error)` |
| Shutdown | Apenas `ShutdownTelemetry()` | `ShutdownTelemetry()` + `ShutdownTelemetryWithContext(ctx) error` |
| Obfuscação | Interface `FieldObfuscator` + `DefaultObfuscator` | `Redactor` com `RedactionRule` (FieldPattern, PathPattern, Action) |
| Ações de redação | Apenas mask (`"********"`) | `RedactionMask`, `RedactionHash` (HMAC-SHA256 por instância), `RedactionDrop` |
| Span processor | `AttrBagSpanProcessor` | Adiciona `RedactingAttrBagSpanProcessor` — redacta atributos sensíveis em tempo real |
| Atributos de span | `SetSpanAttributesFromStruct` (deprecated) | `SetSpanAttributesFromValue` + `BuildAttributesFromValue` com cap de profundidade (32) e cap de atributos (128) |

**Migração:**
```go
// Antes
tl := opentelemetry.InitializeTelemetry(&opentelemetry.TelemetryConfig{...})

// Depois
tl, err := opentelemetry.NewTelemetry(opentelemetry.TelemetryConfig{...})
if err != nil {
    return err
}
tl.ApplyGlobals() // opt-in
```

A mudança mais importante na telemetria: `FieldObfuscator` foi removido e substituído pelo `Redactor` com regras compostas. O `RedactionHash` usa uma chave HMAC gerada via `crypto/rand` por instância do `Redactor` — isso previne ataques de rainbow table em PII de baixa entropia como endereços de e-mail.

---

### `otel/metrics` — Error Returns, NopFactory, Sem Positional Args de Org/Ledger

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| Construção da factory | `NewMetricsFactory(meter, logger) *MetricsFactory` (panics em erro) | Retorna `(*MetricsFactory, error)` |
| Nop factory | Não existe | `NewNopFactory() *MetricsFactory` — para testes e métricas desabilitadas |
| Recorders de domínio | `RecordAccountCreated(ctx, orgID, ledgerID, ...)` — positional args | Sem positional org/ledger — use `...attribute.KeyValue` |
| Builders | Mutáveis | Imutáveis — cada `WithLabels`/`WithAttributes` retorna um novo snapshot do builder |

---

### `circuitbreaker` — Validação, Métricas, Health Checker Seguro

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| `GetOrCreate` | Retorna `CircuitBreaker` (sem erro) | Retorna `(CircuitBreaker, error)` |
| Validação de config | `NewHealthChecker` panics em intervalo inválido | `NewHealthCheckerWithValidation` retorna erro |
| Métricas | Não existe | Option `WithMetricsFactory(f)` |
| `IsHealthy` | Não existe | `IsHealthy(serviceName) bool` — apenas estado `Closed` é saudável |
| `Execute` no Manager | Não existe | `(Manager).Execute(serviceName, fn)` — execução direta sem `GetOrCreate` |

---

### `redis` — Topologia Declarativa, GCP IAM, Lock API Tipada

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| Config | Struct flat com `Mode string` | Struct `Topology` com sub-structs `Standalone`, `Sentinel`, `Cluster` — exactly-one pattern |
| Auth | `Password string` e `UseGCPIAMAuth bool` misturados na config | Struct `Auth` com `StaticPassword *StaticPasswordAuth` ou `GCPIAM *GCPIAMAuth` |
| Redação de credenciais | `Password` em texto plano no struct | `StaticPasswordAuth.String()` e `GCPIAMAuth.String()` redactam — seguro para `%v` acidental |
| Logger de pacote | Não existe | `SetPackageLogger(l log.Logger)` via `atomic.Value` — para diagnósticos de nil-receiver |
| `Status()` | Não existe | Retorna `Status{Connected, LastRefreshError, LastRefreshAt, RefreshLoopRunning}` |
| Lock API | `DistributedLocker` com `*redsync.Mutex` exposto | `LockManager` com interface `LockHandle` — mutex não exposto |
| Retorno de `TryLock` | `(*redsync.Mutex, bool, error)` | `(LockHandle, bool, error)` — distingue contention de erro real |
| Validação de lock options | Não validado | Estrito: max 1000 tries, drift factor em [0,1), expiry deve ser positivo |

---

### `postgres` — SanitizedError, Validação de DSN, Segurança de Migrations

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| API de connect | `Connect() error` | `Connect(ctx) error` + `Resolver(ctx)` para lazy reconnect |
| Acesso ao DB | `GetDB() (dbresolver.DB, error)` | `Resolver(ctx)` (lazy reconnect) + `Primary() (*sql.DB, error)` |
| Config de migration | Embutida no `PostgresConnection` | Struct `MigrationConfig` separado + `NewMigrator(cfg)` |
| Sanitização de erros | Erros de DB podem conter DSN | `SanitizedError` — `Unwrap()` retorna deliberadamente `nil` para quebrar a error chain e prevenir vazamento de credenciais |
| Validação de DSN | Não valida | Valida formatos URL e `key=value`; avisa sobre `sslmode=disable` |
| Path traversal | Não protegido | `sanitizePath` rejeita componentes `..` |

**O padrão `SanitizedError.Unwrap() nil` é intencional:** `errors.Is` não consegue traversar para o erro original, que pode conter credenciais do DSN na mensagem. Isso é comportamento documentado, não um bug.

---

### `mongo` — Backoff Rate-Limiting, TLS, URI Builder

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| API | `MongoConnection` com `GetDB()` | `Client` com `Client()` e `ResolveClient()` (lazy + backoff) |
| Reconexão | Tentativas sem limitação | `ResolveClient` rastreia `connectAttempts`, aplica backoff exponencial com cap de 30s |
| TLS | Não suportado | `TLSConfig` com CA cert em base64, versão mínima configurável (padrão TLS 1.2) |
| URI builder | Não existe | `BuildURI(URIConfig) (string, error)` com 8 tipos distintos de erro |
| Limpeza de credenciais | Não existe | `c.cfg.URI = ""` após connect — credenciais não ficam na memória |
| `EnsureIndexes` | Falha no primeiro erro | Coleta todos os erros via `errors.Join` |

---

### `server` — API Expandida, Hooks, Canal `ServersStarted`

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| `GracefulShutdown` | Existe | Removido (breaking) |
| `ServerManager` | `WithHTTPServer`, `WithGRPCServer`, `StartWithGracefulShutdown` | Adiciona `WithShutdownChannel`, `WithShutdownTimeout`, `WithShutdownHook` |
| Retorno de erro | `StartWithGracefulShutdown()` void | `StartWithGracefulShutdownWithError() error` — novo |
| Coordenação em testes | Não existe | `ServersStarted() <-chan struct{}` — sinaliza quando os servidores estão escutando |

---

### `net/http` — API Unificada, SSRF, Tipos de Cursor, Verificação de Ownership

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| Helpers de response | ~15 funções individuais (`Unauthorized`, `BadRequest`, `Created`, ...) | Removidas; substituídas por `Respond`, `RespondStatus`, `RespondError`, `RenderError` |
| `Ping` | Retorna `"healthy"` | Retorna `"pong"` |
| `ErrorResponse.Code` | `string` | `int` |
| Proxy reverso SSRF | Sem proteção | `ReverseProxyPolicy` com allowlist de schemes/hosts, bloqueio de IPs privados, sem seguimento de redirecionamentos |
| Paginação por cursor | `Cursor{ID, PointsNext}` único | `TimestampCursor`, `SortCursor`, funções de encode/decode |
| Verificação de ownership | Não existe | `ParseAndVerifyTenantScopedID`, `ParseAndVerifyResourceScopedID` com func types `TenantOwnershipVerifier` e `ResourceOwnershipVerifier` |
| Tags de validação customizadas | Não existe | `positive_decimal`, `positive_amount`, `nonnegative_amount` registradas no validator |
| CORS | Lê env vars implicitamente | Options funcionais: `WithCORSLogger`, etc. |

**Migração — helpers de response:**
```go
// Antes
return http.OK(c, payload)
return http.BadRequest(c, payload)
return http.NotFound(c, "0042", "Conta Não Encontrada", "conta não existe")

// Depois
return http.Respond(c, fiber.StatusOK, payload)
return http.Respond(c, fiber.StatusBadRequest, payload)
return http.RespondError(c, fiber.StatusNotFound, "Conta Não Encontrada", "conta não existe")
```

---

### `rabbitmq` — Context-Aware, Segurança TLS, Sanitização de Credenciais

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| API | Sem variantes `Context` | Todos os métodos têm variantes `*Context(ctx)` |
| TLS | Sem validação | `AllowInsecureTLS bool` — flag explícita; retorna `ErrInsecureTLS` se não setada |
| Validação de URL de health check | Não valida | Valida scheme, host e rejeita credenciais embutidas |
| Sanitização de credenciais | Erros podem conter password | `sanitizeAMQPErr` usa `url.URL.Redacted()` antes de logar |
| IPv6 | Não suportado | `BuildRabbitMQConnectionString` coloca `[]` em endereços IPv6 |

---

### `crypto` — Nil Safety, Redação de Credenciais

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| Nil receiver | Panics | Todos os métodos checam `c == nil` primeiro |
| `String()`/`GoString()` | Não implementados | Implementados — retorna `"Crypto{keys:REDACTED}"` |

---

### `license` — Sem Panic, Mais Opções de Terminação

| Aspecto | lib-commons | lib-uncommons |
|---|---|---|
| `DefaultHandler` | Panics | Registra falha de assertion, sem panic |
| `New()` | Sem options | `New(opts ...ManagerOption)` com `WithLogger(l)` |
| `TerminateWithError` | Não existe | Retorna error sem invocar o handler |
| `TerminateSafe` | Não existe | Invoca o handler + retorna error se não inicializado |

---

### `constants` — Novos Grupos, Função Utilitária

| Adição | Detalhe |
|---|---|
| `SanitizeMetricLabel(value) string` | Trunca labels OTEL para 64 caracteres — `MaxMetricLabelLength = 64` |
| `CodeMetadataKeyLengthExceeded` / `CodeMetadataValueLengthExceeded` | Novos códigos de erro (0050, 0051) |
| `CodeOnHoldExternalAccount` (0098) | Novo |
| `AttrPrefixAppRequest`, `AttrPrefixAssertion`, `AttrPrefixPanic` | Prefixos de atributo OTEL padronizados |
| `MetricPanicRecoveredTotal`, `MetricAssertionFailedTotal` | Nomes de métricas como constantes |
| `EventAssertionFailed`, `EventPanicRecovered` | Nomes de eventos OTEL como constantes |
| `RateLimitLimit`, `RateLimitRemaining`, `RateLimitReset` | Headers de rate limit padronizados |
| `NOTED` | Novo status de transação |

---

## 4. Padrões Transversais

### Tratamento de Erros — De Falha Silenciosa para Falha Explícita

A `lib-commons` usava `panic` em caminhos de bootstrap (`InitializeTelemetry`, `NewHealthChecker`, `NewMetricsFactory`). A `lib-uncommons` converte todos para `(T, error)`:

- Testes agora conseguem testar cenários de falha
- Serviços podem fazer graceful degradation ao invés de crashar
- Erros são tratados na camada que tem contexto para decidir o que fazer

### Nil Safety — Sistemática em Toda a Biblioteca

Na `lib-commons`, nil receivers causavam panics. Na `lib-uncommons`:
- Todos os métodos principais fazem `if receiver == nil` antes de operar
- `NewNop()`, `NewNopFactory()` fornecem no-ops que satisfazem interfaces
- Helpers de resolução de contexto (`resolveTracer`, `resolveMetricFactory`, `resolveLogger`) retornam no-ops, nunca nil

### Redação de Credenciais — Uma Feature de Segurança de Primeira Classe

| Local | Proteção adicionada |
|---|---|
| `Crypto.String()`/`GoString()` | Redacta chaves |
| `StaticPasswordAuth.String()` | Redacta password |
| `GCPIAMAuth.String()` | Redacta credenciais GCP |
| `SanitizedError.Unwrap() nil` | Quebra a error chain para prevenir vazamento de DSN |
| `sanitizeAMQPErr` | Usa `url.URL.Redacted()` |
| `mongo: c.cfg.URI = ""` | Zera o URI após connect |
| `log.SafeError` | Em produção, loga apenas o _tipo_ do erro, nunca a mensagem |

### Concorrência Segura — Patterns Explícitos

- Redis `SetPackageLogger` usa `atomic.Value` (lock-free)
- Circuit breaker `GetOrCreate` usa double-checked locking corretamente
- Metrics factory usa `sync.Map` com `LoadOrStore`
- Reconexão Redis/Mongo tem backoff com rate-limiting via `connectAttempts`
- `Asserter` usa `sync.RWMutex` para o handler de license

---

## 5. Breaking Changes Intencionais (Remoções)

| Removido | Substituição |
|---|---|
| Struct `GracefulShutdown` | `ServerManager` com `WithShutdownHook`, `WithShutdownChannel` |
| Interface `FieldObfuscator` | `Redactor` com `RedactionRule` |
| `InitializeTelemetry` | `NewTelemetry` (retorna error) |
| `NewHealthChecker` (panics) | `NewHealthCheckerWithValidation` (retorna error) |
| `GetDB()` (mongo) | `Client()` e `ResolveClient()` |
| `GetDB()` (postgres) | `Resolver(ctx)` |
| 15 funções HTTP individuais de response | `Respond`, `RespondStatus`, `RespondError`, `RenderError` |
| `WithOrganizationLabels`/`WithLedgerLabels` (metrics) | Use `...attribute.KeyValue` diretamente |
| Positional args `orgID`, `ledgerID` nos domain recorders | Use `...attribute.KeyValue` diretamente |
| Alias `HealthSimple` | Use `Ping` diretamente |
| Redis key builders (`GenericInternalKey`, etc.) | Movidos para o domínio da aplicação |
| Validações de domínio (`ValidateCountryAddress`, `ValidateAccountType`, etc.) | Movidas para o Midaz |
| Métodos de log estilo printf (`Info`, `Infof`, `Fatal`, ...) | Interface `Logger` de 5 métodos |
| `StringToInt` (sem error return) | `StringToIntOrDefault` |
| `GenerateUUIDv7()` sem error return | Retorna `(uuid.UUID, error)` |
| `Token.Valid` (jwt) | `Token.SignatureValid bool` — deixa o escopo claro |

---

## 6. Resumo dos Princípios de Design

A `lib-uncommons` é uma reescrita guiada por três princípios que estavam ausentes ou inconsistentes na `lib-commons`:

**1. Falhas explícitas**  
Tudo que pode falhar retorna `error`. Zero panics em código de produção — inclusive no bootstrap, onde antes eram aceitáveis. Isso torna os cenários de falha testáveis e dá à camada chamadora o poder de decidir como se recuperar.

**2. Segurança por padrão**  
Credenciais são redactadas no `String()`, DSNs são zerados após a conexão, error chains são deliberadamente quebradas para não vazar secrets, e PII em spans é hasheada com uma chave HMAC por instância. Essas não são features opt-in — são o comportamento padrão.

**3. Observabilidade integrada**  
Assertions, panics e mudanças de estado do circuit breaker emitem métricas OTEL e span events automaticamente — sem que o código de aplicação precise instrumentá-los explicitamente. Observabilidade é infraestrutura, não responsabilidade do chamador.

Os novos pacotes (`assert`, `backoff`, `cron`, `errgroup`, `jwt`, `runtime`, `safe`) fecham lacunas que antes forçavam cada serviço a reimplementar os mesmos padrões — com resultados inconsistentes e níveis variados de segurança.
