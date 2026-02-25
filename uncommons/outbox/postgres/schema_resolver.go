package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	libCommons "github.com/LerianStudio/lib-uncommons/v2/uncommons"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/outbox"
	libPostgres "github.com/LerianStudio/lib-uncommons/v2/uncommons/postgres"
)

const uuidSchemaRegex = "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"

var ErrDefaultTenantIDInvalid = errors.New("default tenant id must be UUID when tenant is required")

type SchemaResolverOption func(*SchemaResolver)

func WithDefaultTenantID(tenantID string) SchemaResolverOption {
	return func(resolver *SchemaResolver) {
		resolver.defaultTenantID = tenantID
	}
}

// WithRequireTenant enforces that every ApplyTenant call receives a non-empty
// tenant ID. This is the default behavior.
func WithRequireTenant() SchemaResolverOption {
	return func(resolver *SchemaResolver) {
		resolver.requireTenant = true
	}
}

// WithAllowEmptyTenant permits ApplyTenant calls with empty tenant IDs.
//
// Use only in trusted single-tenant contexts where falling back to `public`
// is explicitly intended. The secure default requires tenant IDs.
func WithAllowEmptyTenant() SchemaResolverOption {
	return func(resolver *SchemaResolver) {
		resolver.requireTenant = false
	}
}

// SchemaResolver applies schema-per-tenant scoping and tenant discovery.
type SchemaResolver struct {
	client          *libPostgres.Client
	defaultTenantID string
	requireTenant   bool
}

func NewSchemaResolver(client *libPostgres.Client, opts ...SchemaResolverOption) (*SchemaResolver, error) {
	if client == nil {
		return nil, ErrConnectionRequired
	}

	resolver := &SchemaResolver{client: client, requireTenant: true}

	for _, opt := range opts {
		if opt != nil {
			opt(resolver)
		}
	}

	resolver.defaultTenantID = strings.TrimSpace(resolver.defaultTenantID)
	if resolver.defaultTenantID != "" && resolver.requireTenant && !libCommons.IsUUID(resolver.defaultTenantID) {
		return nil, ErrDefaultTenantIDInvalid
	}

	return resolver, nil
}

func (resolver *SchemaResolver) RequiresTenant() bool {
	if resolver == nil {
		return true
	}

	return resolver.requireTenant
}

// ApplyTenant scopes the current transaction to tenant search_path.
//
// Security invariant: tenantID must remain UUID-validated and identifier-quoted
// before query construction. This method intentionally relies on both checks to
// keep dynamic search_path assignment safe.
func (resolver *SchemaResolver) ApplyTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if resolver == nil {
		return ErrConnectionRequired
	}

	if tx == nil {
		return ErrTransactionRequired
	}

	tenantID = strings.TrimSpace(tenantID)

	if tenantID == "" {
		if resolver.requireTenant {
			return fmt.Errorf("schema resolver: %w", outbox.ErrTenantIDRequired)
		}

		return nil
	}

	if tenantID == resolver.defaultTenantID && !resolver.requireTenant {
		return nil
	}

	if !libCommons.IsUUID(tenantID) {
		return errors.New("invalid tenant id format")
	}

	query := "SET LOCAL search_path TO " + quoteIdentifier(tenantID) + ", public"
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("set search_path: %w", err)
	}

	return nil
}

// DiscoverTenants returns tenants by inspecting schema names.
func (resolver *SchemaResolver) DiscoverTenants(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if resolver == nil || resolver.client == nil {
		return nil, ErrConnectionRequired
	}

	db, err := resolver.primaryDB(ctx)
	if err != nil {
		return nil, err
	}

	query := "SELECT nspname FROM pg_namespace WHERE nspname ~* $1"

	rows, err := db.QueryContext(ctx, query, uuidSchemaRegex)
	if err != nil {
		return nil, fmt.Errorf("querying tenant schemas: %w", err)
	}
	defer rows.Close()

	tenants := make([]string, 0)

	for rows.Next() {
		var tenant string
		if scanErr := rows.Scan(&tenant); scanErr != nil {
			return nil, fmt.Errorf("scanning tenant schema: %w", scanErr)
		}

		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tenant schemas: %w", err)
	}

	if resolver.defaultTenantID != "" &&
		!slices.Contains(tenants, resolver.defaultTenantID) &&
		(!resolver.requireTenant || libCommons.IsUUID(resolver.defaultTenantID)) {
		tenants = append(tenants, resolver.defaultTenantID)
	}

	return tenants, nil
}

func (resolver *SchemaResolver) primaryDB(ctx context.Context) (*sql.DB, error) {
	return resolvePrimaryDB(ctx, resolver.client)
}
