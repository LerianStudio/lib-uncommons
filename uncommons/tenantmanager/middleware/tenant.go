package middleware

import (
	"context"
	"errors"
	"net/http"

	libCommons "github.com/LerianStudio/lib-uncommons/v2/uncommons"
	liblog "github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	libHTTP "github.com/LerianStudio/lib-uncommons/v2/uncommons/net/http"
	libOpentelemetry "github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/core"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/internal/logcompat"
	tmmongo "github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/mongo"
	tmpostgres "github.com/LerianStudio/lib-uncommons/v2/uncommons/tenantmanager/postgres"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// TenantMiddleware extracts tenantId from JWT token and resolves the database connection.
// It stores the connection in context for downstream handlers and repositories.
// Supports PostgreSQL only, MongoDB only, or both databases.
type TenantMiddleware struct {
	postgres *tmpostgres.Manager // PostgreSQL manager (optional)
	mongo    *tmmongo.Manager    // MongoDB manager (optional)
	enabled  bool
}

// TenantMiddlewareOption configures a TenantMiddleware.
type TenantMiddlewareOption func(*TenantMiddleware)

// WithPostgresManager sets the PostgreSQL manager for the tenant middleware.
// When configured, the middleware will resolve PostgreSQL connections for tenants.
func WithPostgresManager(postgres *tmpostgres.Manager) TenantMiddlewareOption {
	return func(m *TenantMiddleware) {
		m.postgres = postgres
		m.enabled = m.postgres != nil || m.mongo != nil
	}
}

// WithMongoManager sets the MongoDB manager for the tenant middleware.
// When configured, the middleware will resolve MongoDB connections for tenants.
func WithMongoManager(mongo *tmmongo.Manager) TenantMiddlewareOption {
	return func(m *TenantMiddleware) {
		m.mongo = mongo
		m.enabled = m.postgres != nil || m.mongo != nil
	}
}

// NewTenantMiddleware creates a new TenantMiddleware with the given options.
// Use WithPostgresManager and/or WithMongoManager to configure which databases to use.
// The middleware is enabled if at least one manager is configured.
//
// Usage examples:
//
//	// PostgreSQL only
//	mid := middleware.NewTenantMiddleware(middleware.WithPostgresManager(pgManager))
//
//	// MongoDB only
//	mid := middleware.NewTenantMiddleware(middleware.WithMongoManager(mongoManager))
//
//	// Both PostgreSQL and MongoDB
//	mid := middleware.NewTenantMiddleware(
//	    middleware.WithPostgresManager(pgManager),
//	    middleware.WithMongoManager(mongoManager),
//	)
func NewTenantMiddleware(opts ...TenantMiddlewareOption) *TenantMiddleware {
	m := &TenantMiddleware{}

	for _, opt := range opts {
		opt(m)
	}

	// Enable if any manager is configured
	m.enabled = m.postgres != nil || m.mongo != nil

	return m
}

// WithTenantDB returns a Fiber handler that extracts tenant context and resolves DB connection.
// It parses the JWT token to get tenantId and fetches the appropriate connection from Tenant Manager.
// The connection is stored in the request context for use by repositories.
//
// Usage in routes.go:
//
//	tenantMid := middleware.NewTenantMiddleware(middleware.WithPostgresManager(pgManager))
//	f.Use(tenantMid.WithTenantDB)
func (m *TenantMiddleware) WithTenantDB(c *fiber.Ctx) error {
	// If middleware is disabled, pass through
	if !m.enabled {
		return c.Next()
	}

	ctx := c.UserContext()

	if ctx == nil {
		ctx = context.Background()
	}

	baseLogger, tracer, _, _ := libCommons.NewTrackingFromContext(ctx)
	logger := logcompat.New(baseLogger)

	ctx, span := tracer.Start(ctx, "middleware.tenant.resolve_db")
	defer span.End()

	// Extract JWT token from Authorization header
	accessToken := libHTTP.ExtractTokenFromHeader(c)
	if accessToken == "" {
		logger.ErrorCtx(ctx, "no authorization token - multi-tenant mode requires JWT with tenantId")
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "missing authorization token",
			core.ErrAuthorizationTokenRequired)

		return unauthorizedError(c, "MISSING_TOKEN", "Authorization token is required")
	}

	if !hasUpstreamAuthAssertion(c) {
		logger.ErrorCtx(ctx, "missing upstream auth assertion; refusing ParseUnverified token path")
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "missing upstream auth assertion", core.ErrAuthorizationTokenRequired)

		return unauthorizedError(c, "UNAUTHORIZED", "Unauthorized")
	}

	// Parse JWT token without signature verification.
	//
	// SECURITY CONTRACT (defense-in-depth): this code path is only valid when upstream
	// lib-auth middleware has already validated signature/issuer/audience and asserted
	// identity into request context (for example c.Locals("user_id") or X-User-ID).
	// hasUpstreamAuthAssertion() enforces that contract and fails closed when missing.
	token, _, err := new(jwt.Parser).ParseUnverified(accessToken, jwt.MapClaims{})
	if err != nil {
		logger.Base().Log(ctx, liblog.LevelError, "failed to parse JWT token", liblog.Err(err))
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "failed to parse token", err)

		return unauthorizedError(c, "INVALID_TOKEN", "Failed to parse authorization token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		logger.ErrorCtx(ctx, "JWT claims are not in expected format")
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "invalid claims format",
			core.ErrInvalidTenantClaims)

		return unauthorizedError(c, "INVALID_TOKEN", "JWT claims are not in expected format")
	}

	// Extract tenantId from claims
	tenantID, _ := claims["tenantId"].(string)
	if tenantID == "" {
		logger.ErrorCtx(ctx, "no tenantId in JWT - multi-tenant mode requires tenantId claim")
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "missing tenantId in JWT",
			core.ErrMissingTenantIDClaim)

		return unauthorizedError(c, "MISSING_TENANT", "tenantId is required in JWT token")
	}

	if !core.IsValidTenantID(tenantID) {
		logger.Base().Log(ctx, liblog.LevelError, "invalid tenantId format in JWT",
			liblog.String("tenant_id", tenantID))
		libOpentelemetry.HandleSpanBusinessErrorEvent(span, "invalid tenantId format",
			core.ErrInvalidTenantClaims)

		return unauthorizedError(c, "INVALID_TENANT", "tenantId has invalid format")
	}

	logger.Base().Log(ctx, liblog.LevelInfo, "tenant context resolved",
		liblog.String("tenant_id", tenantID))

	// Store tenant ID in context
	ctx = core.ContextWithTenantID(ctx, tenantID)

	// Handle PostgreSQL if manager is configured
	if m.postgres != nil {
		conn, err := m.postgres.GetConnection(ctx, tenantID)
		if err != nil {
			var suspErr *core.TenantSuspendedError
			if errors.As(err, &suspErr) {
				logger.Base().Log(ctx, liblog.LevelWarn, "tenant service suspended",
					liblog.String("status", suspErr.Status),
					liblog.String("tenant_id", tenantID))
				libOpentelemetry.HandleSpanBusinessErrorEvent(span, "tenant service suspended", err)

				return forbiddenError(c, "0131", "Service Suspended",
					"tenant service is "+suspErr.Status)
			}

			logger.Base().Log(ctx, liblog.LevelError, "failed to get tenant PostgreSQL connection", liblog.Err(err))
			libOpentelemetry.HandleSpanError(span, "failed to get tenant PostgreSQL connection", err)

			return internalServerError(c, "TENANT_DB_ERROR", "Failed to resolve tenant database")
		}

		// Get the database connection from PostgresConnection
		db, err := conn.GetDB()
		if err != nil {
			logger.Base().Log(ctx, liblog.LevelError, "failed to get database from PostgreSQL connection", liblog.Err(err))
			libOpentelemetry.HandleSpanError(span, "failed to get database from PostgreSQL connection", err)

			return internalServerError(c, "TENANT_DB_ERROR", "Failed to get tenant database connection")
		}

		// Store PostgreSQL connection in context
		ctx = core.ContextWithTenantPGConnection(ctx, db)
	}

	// Handle MongoDB if manager is configured
	if m.mongo != nil {
		mongoDB, err := m.mongo.GetDatabaseForTenant(ctx, tenantID)
		if err != nil {
			var suspErr *core.TenantSuspendedError
			if errors.As(err, &suspErr) {
				logger.Base().Log(ctx, liblog.LevelWarn, "tenant service suspended",
					liblog.String("status", suspErr.Status),
					liblog.String("tenant_id", tenantID))
				libOpentelemetry.HandleSpanBusinessErrorEvent(span, "tenant service suspended", err)

				return forbiddenError(c, "0131", "Service Suspended",
					"tenant service is "+suspErr.Status)
			}

			logger.Base().Log(ctx, liblog.LevelError, "failed to get tenant MongoDB connection", liblog.Err(err))
			libOpentelemetry.HandleSpanError(span, "failed to get tenant MongoDB connection", err)

			return internalServerError(c, "TENANT_MONGO_ERROR", "Failed to resolve tenant MongoDB database")
		}

		ctx = core.ContextWithTenantMongo(ctx, mongoDB)
	}

	// Update Fiber context
	c.SetUserContext(ctx)

	return c.Next()
}

// hasUpstreamAuthAssertion verifies that upstream auth middleware has run
// by checking the server-side local value. HTTP headers are NOT checked
// as they are spoofable by clients.
func hasUpstreamAuthAssertion(c *fiber.Ctx) bool {
	if c == nil {
		return false
	}

	if userID, ok := c.Locals("user_id").(string); ok && userID != "" {
		return true
	}

	return false
}

// forbiddenError sends an HTTP 403 Forbidden response.
// Used when the tenant-service association exists but is not active (suspended or purged).
func forbiddenError(c *fiber.Ctx, code, title, message string) error {
	return c.Status(http.StatusForbidden).JSON(fiber.Map{
		"code":    code,
		"title":   title,
		"message": message,
	})
}

// internalServerError sends an HTTP 500 Internal Server Error response.
func internalServerError(c *fiber.Ctx, code, title string) error {
	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
		"code":    code,
		"title":   title,
		"message": "Internal server error",
	})
}

// unauthorizedError sends an HTTP 401 Unauthorized response.
func unauthorizedError(c *fiber.Ctx, code, message string) error {
	return c.Status(http.StatusUnauthorized).JSON(fiber.Map{
		"code":    code,
		"title":   "Unauthorized",
		"message": message,
	})
}

// Enabled returns whether the middleware is enabled.
func (m *TenantMiddleware) Enabled() bool {
	return m.enabled
}
