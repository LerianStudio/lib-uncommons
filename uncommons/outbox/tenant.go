package outbox

import (
	"context"
	"database/sql"
	"strings"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/core"
)

type tenantIDContextKey string

// TenantIDContextKey stores tenant id used by outbox multi-tenant operations.
// Deprecated: use tenantmanager/core.ContextWithTenantID and tenantmanager/core.GetTenantIDFromContext.
// This constant will be removed in v3.0.
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

	if hasTenantIDOuterSpaces(tenantID) {
		return ctx
	}

	ctx = core.ContextWithTenantID(ctx, tenantID)

	return context.WithValue(ctx, TenantIDContextKey, tenantID)
}

// TenantIDFromContext reads tenant id from context.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	tenantID := core.GetTenantIDFromContext(ctx)
	if hasTenantIDOuterSpaces(tenantID) {
		return "", false
	}

	if tenantID != "" {
		return tenantID, true
	}

	tenantID, ok := ctx.Value(TenantIDContextKey).(string)
	if !ok || tenantID == "" || hasTenantIDOuterSpaces(tenantID) {
		return "", false
	}

	return tenantID, true
}

func hasTenantIDOuterSpaces(tenantID string) bool {
	return tenantID != strings.TrimSpace(tenantID)
}
