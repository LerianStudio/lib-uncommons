# lib-uncommons

A comprehensive Go library providing uncommon utilities and components for building robust microservices and applications in the Lerian Studio ecosystem.

## Overview

`lib-uncommons` is a utility library that provides a collection of reusable components and helpers for Go applications. It includes standardized implementations for database connections, message queuing, logging, context management, error handling, transaction processing, and more.

## Features

### Core Components

- **App Management**: Framework for managing application lifecycle and runtime (`app.go`)
- **Context Utilities**: Enhanced context management with support for logging, tracing, and header IDs (`context.go`)
- **Error Handling**: Standardized business error handling and responses (`errors.go`)

### Database Connectors

- **PostgreSQL**: Connection management, migrations, and utilities for PostgreSQL databases
- **MongoDB**: Connection management and utilities for MongoDB
- **Redis**: Client implementation and utilities for Redis

### Messaging

- **RabbitMQ**: Client implementation and utilities for RabbitMQ

### Observability

- **Logging**: Pluggable logging interface with multiple implementations
- **Logging Obfuscation**: Dynamic environment variable to obfuscate specific fields from the request payload logging
  - `SECURE_LOG_FIELDS=password,apiKey`
- **OpenTelemetry**: Integrated tracing, metrics, and logs through OpenTelemetry
- **Zap**: Integration with Uber's Zap logging library

### Utilities

- **String Utilities**: Common string manipulation functions
- **Type Conversion**: Safe type conversion utilities
- **Time Helpers**: Date and time manipulation functions
- **OS Utilities**: Operating system related utilities
- **Pointer Utilities**: Helper functions for pointer type operations
- **Transaction Processing**: Utilities for financial transaction processing and validation

### Resilience

- **Circuit Breaker**: Circuit breaker pattern with health checking and state management
- **Backoff**: Exponential backoff implementation
- **Rate Limiting**: Redis-backed rate limiter with Fiber middleware support
- **Distributed Locking**: RedLock algorithm for distributed lock management

### Safety

- **Safe Math**: Overflow-protected arithmetic operations
- **Safe Regex**: Safe regular expression utilities
- **Safe Slices**: Nil-safe slice operations
- **Assertion Framework**: Production-safe assertions with telemetry integration

### Runtime

- **Goroutine Management**: Safe goroutine launching with panic recovery
- **Panic Metrics**: OpenTelemetry metrics for recovered panics
- **Error Reporting**: Pluggable error reporter integration (e.g., Sentry)

## Getting Started

### Prerequisites

- Go 1.25 or higher

### Installation

```bash
go get github.com/LerianStudio/lib-uncommons
```

## Usage

```go
import (
    "github.com/LerianStudio/lib-uncommons/uncommons"
    "github.com/LerianStudio/lib-uncommons/uncommons/log"
    "github.com/LerianStudio/lib-uncommons/uncommons/postgres"
    "github.com/LerianStudio/lib-uncommons/uncommons/redis"
    "github.com/LerianStudio/lib-uncommons/uncommons/opentelemetry"
)
```

## Contributing

Please read the contributing guidelines before submitting pull requests.

## License

This project is licensed under the terms found in the LICENSE file in the root directory.
