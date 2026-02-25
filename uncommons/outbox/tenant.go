package outbox

import (
	"context"
	"database/sql"
	"strings"
)

type tenantIDContextKey string

// TenantIDContextKey stores tenant id used by outbox multi-tenant operations.
const TenantIDContextKey tenantIDContextKey = "outbox.tenant_id"

// TenantResolver applies tenant-scoping rules for a transaction.
type TenantResolver interface {
	ApplyTenant(ctx context.Context, tx *sql.Tx, tenantID string) error
}

// TenantDiscoverer lists tenant identifiers to dispatch events for.
type TenantDiscoverer interface {
	DiscoverTenants(ctx context.Context) ([]string, error)
}

// ContextWithTenantID returns a context carrying tenantID.
func ContextWithTenantID(ctx context.Context, tenantID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	return context.WithValue(ctx, TenantIDContextKey, strings.TrimSpace(tenantID))
}

// TenantIDFromContext reads tenant id from context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	tenantID, ok := ctx.Value(TenantIDContextKey).(string)
	if !ok || strings.TrimSpace(tenantID) == "" {
		return "", false
	}

	return strings.TrimSpace(tenantID), true
}
