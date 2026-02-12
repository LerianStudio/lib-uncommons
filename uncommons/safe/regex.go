package safe

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
)

// ErrInvalidRegex is returned when a regex pattern cannot be compiled.
var ErrInvalidRegex = errors.New("invalid regular expression")

// maxCacheSize is the upper bound for cached compiled regex patterns.
// When this limit is reached, the entire cache is cleared to prevent
// unbounded memory growth from dynamic user-provided patterns.
const maxCacheSize = 1024

// regexCache caches compiled regex patterns for performance.
// Protected by regexMu; bounded to maxCacheSize entries.
var (
	regexMu    sync.RWMutex
	regexCache = make(map[string]*regexp.Regexp)
)

// cacheLoad returns a cached regex and true if it exists, or nil and false.
func cacheLoad(key string) (*regexp.Regexp, bool) {
	regexMu.RLock()
	defer regexMu.RUnlock()

	re, ok := regexCache[key]

	return re, ok
}

// cacheStore stores a compiled regex, clearing the cache first if it is full.
func cacheStore(key string, re *regexp.Regexp) {
	regexMu.Lock()
	defer regexMu.Unlock()

	if len(regexCache) >= maxCacheSize {
		regexCache = make(map[string]*regexp.Regexp)
	}

	regexCache[key] = re
}

// Compile compiles a regex pattern with error return instead of panic.
// Compiled patterns are cached for performance.
//
// Use this for dynamic patterns (e.g., user-provided patterns).
// For static compile-time patterns, use regexp.MustCompile directly.
//
// Example:
//
//	re, err := safe.Compile(userPattern)
//	if err != nil {
//	    return fmt.Errorf("invalid pattern: %w", err)
//	}
//	matches := re.FindAllString(input, -1)
func Compile(pattern string) (*regexp.Regexp, error) {
	if cached, ok := cacheLoad(pattern); ok {
		return cached, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRegex, err)
	}

	cacheStore(pattern, re)

	return re, nil
}

// CompilePOSIX compiles a POSIX ERE regex pattern with error return.
// Compiled patterns are cached for performance.
//
// Example:
//
//	re, err := safe.CompilePOSIX(userPattern)
//	if err != nil {
//	    return fmt.Errorf("invalid POSIX pattern: %w", err)
//	}
func CompilePOSIX(pattern string) (*regexp.Regexp, error) {
	cacheKey := "posix:" + pattern

	if cached, ok := cacheLoad(cacheKey); ok {
		return cached, nil
	}

	re, err := regexp.CompilePOSIX(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRegex, err)
	}

	cacheStore(cacheKey, re)

	return re, nil
}

// MatchString compiles and matches a pattern against input in one call.
// Returns false if the pattern is invalid.
//
// Example:
//
//	matched, err := safe.MatchString(`^\d{4}-\d{2}-\d{2}$`, dateStr)
//	if err != nil {
//	    return fmt.Errorf("invalid date pattern: %w", err)
//	}
func MatchString(pattern, input string) (bool, error) {
	re, err := Compile(pattern)
	if err != nil {
		return false, err
	}

	return re.MatchString(input), nil
}

// FindString compiles and finds the first match.
// Returns empty string if pattern is invalid or no match found.
//
// Example:
//
//	match, err := safe.FindString(`[a-z]+`, input)
//	if err != nil {
//	    return fmt.Errorf("invalid pattern: %w", err)
//	}
func FindString(pattern, input string) (string, error) {
	re, err := Compile(pattern)
	if err != nil {
		return "", err
	}

	return re.FindString(input), nil
}

// ClearCache clears the regex cache. Useful for testing.
func ClearCache() {
	regexMu.Lock()
	defer regexMu.Unlock()

	regexCache = make(map[string]*regexp.Regexp)
}
