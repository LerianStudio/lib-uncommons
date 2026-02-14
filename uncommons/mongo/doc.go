// Package mongo provides resilient MongoDB connection and index management helpers.
//
// The package wraps connection lifecycle concerns (connect, ping, close) and offers
// EnsureIndexes for idempotent index creation with structured error reporting.
package mongo
