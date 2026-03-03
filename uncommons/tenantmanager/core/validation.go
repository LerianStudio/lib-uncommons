package core

import "regexp"

// MaxTenantIDLength is the maximum allowed length for a tenant ID.
const MaxTenantIDLength = 256

// ValidTenantIDPattern enforces a character whitelist for tenant IDs.
// Only alphanumeric characters, hyphens, and underscores are allowed.
// The first character must be alphanumeric.
var ValidTenantIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// IsValidTenantID validates a tenant ID against security constraints.
// Valid tenant IDs must be non-empty, at most MaxTenantIDLength characters,
// and match ValidTenantIDPattern.
func IsValidTenantID(id string) bool {
	if id == "" || len(id) > MaxTenantIDLength {
		return false
	}

	return ValidTenantIDPattern.MatchString(id)
}
