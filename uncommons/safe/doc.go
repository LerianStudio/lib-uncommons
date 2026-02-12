// Package safe provides safe wrappers for operations that could panic.
//
// This package implements the "Safe Patterns" layer of the panic prevention strategy:
//   - Divide: Prevents division by zero panics in decimal operations
//   - Assert: Type-safe generic assertions without panics
//   - Compile: Safe regex compilation with caching
//   - First/Last/At: Safe slice access with bounds checking
//
// All functions return errors instead of panicking, enabling graceful error handling
// and maintaining service stability.
//
// Example usage:
//
//	result, err := safe.Divide(ctx, numerator, denominator)
//	if err != nil {
//	    return fmt.Errorf("calculate percentage: %w", err)
//	}
//
// See also:
//   - pkg/assert: Assertion utilities for validating invariants
//   - pkg/runtime: Panic recovery infrastructure
package safe
