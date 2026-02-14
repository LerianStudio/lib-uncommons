package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	// File system migration source. We need to import it to be able to use it as source in migrate.NewWithSourceInstance

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/assert"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/runtime"
	"github.com/bxcodec/dbresolver/v2"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 10
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
)

var (
	// ErrNilClient is returned when a postgres client receiver is nil.
	ErrNilClient = errors.New("postgres client is nil")
	// ErrNilContext is returned when a required context is nil.
	ErrNilContext = errors.New("context is nil")
	// ErrInvalidConfig indicates invalid postgres or migration configuration.
	ErrInvalidConfig = errors.New("invalid postgres config")
	// ErrNotConnected indicates operations requiring an active connection were called before connect.
	ErrNotConnected = errors.New("postgres client is not connected")
	// ErrInvalidDatabaseName indicates an invalid database identifier.
	ErrInvalidDatabaseName = errors.New("invalid database name")
	// ErrMigrationDirty indicates migrations stopped at a dirty version.
	ErrMigrationDirty = errors.New("postgres migration dirty")

	dbOpenFn = sql.Open

	createResolverFn = func(primaryDB, replicaDB *sql.DB) (_ dbresolver.DB, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				runtime.HandlePanicValue(context.Background(), log.NewNop(), recovered, "postgres", "create_resolver")
				err = fmt.Errorf("failed to create resolver: %v", recovered)
			}
		}()

		connectionDB := dbresolver.New(
			dbresolver.WithPrimaryDBs(primaryDB),
			dbresolver.WithReplicaDBs(replicaDB),
			dbresolver.WithLoadBalancer(dbresolver.RoundRobinLB),
		)

		if connectionDB == nil {
			return nil, errors.New("resolver returned nil connection")
		}

		return connectionDB, nil
	}

	runMigrationsFn = runMigrations

	connectionStringCredentialsPattern = regexp.MustCompile(`://[^@\s]+@`)
	connectionStringPasswordPattern    = regexp.MustCompile(`(?i)(password=)([^\s&]+)`)
	dbNamePattern                      = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)
)

// Config stores immutable connection options for a postgres client.
type Config struct {
	PrimaryDSN         string
	ReplicaDSN         string
	Logger             log.Logger
	MaxOpenConnections int
	MaxIdleConnections int
}

func (c Config) withDefaults() Config {
	if c.Logger == nil {
		c.Logger = log.NewNop()
	}

	if c.MaxOpenConnections <= 0 {
		c.MaxOpenConnections = defaultMaxOpenConns
	}

	if c.MaxIdleConnections <= 0 {
		c.MaxIdleConnections = defaultMaxIdleConns
	}

	return c
}

func (c Config) validate() error {
	if strings.TrimSpace(c.PrimaryDSN) == "" {
		return fmt.Errorf("%w: primary dsn cannot be empty", ErrInvalidConfig)
	}

	if strings.TrimSpace(c.ReplicaDSN) == "" {
		return fmt.Errorf("%w: replica dsn cannot be empty", ErrInvalidConfig)
	}

	return nil
}

// Client is the v2 postgres connection manager.
type Client struct {
	mu       sync.RWMutex
	cfg      Config
	resolver dbresolver.DB
	primary  *sql.DB
	replica  *sql.DB
}

// New creates a postgres client with immutable configuration.
func New(cfg Config) (*Client, error) {
	cfg = cfg.withDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("postgres new: %w", err)
	}

	return &Client{cfg: cfg}, nil
}

func (c *Client) logger() log.Logger {
	if c == nil || c.cfg.Logger == nil {
		return log.NewNop()
	}

	return c.cfg.Logger
}

// Connect establishes a new primary/replica resolver and swaps it atomically.
func (c *Client) Connect(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("postgres connect: %w", ErrNilClient)
	}

	if ctx == nil {
		return fmt.Errorf("postgres connect: %w", ErrNilContext)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.connectLocked(ctx)
}

// connectLocked performs the actual connection logic.
// The caller MUST hold c.mu (write lock) before calling this method.
func (c *Client) connectLocked(ctx context.Context) error {
	primary, replica, resolver, err := c.buildConnection(ctx)
	if err != nil {
		return err
	}

	oldResolver := c.resolver
	oldPrimary := c.primary
	oldReplica := c.replica

	c.resolver = resolver
	c.primary = primary
	c.replica = replica

	if oldResolver != nil {
		if err := oldResolver.Close(); err != nil {
			logf(c.logger(), log.LevelWarn, "failed to close previous resolver after swap: %v", err)
		}
	}

	// Always close old primary/replica explicitly to prevent leaks.
	// The resolver may not own the underlying sql.DB connections.
	_ = closeDB(oldPrimary)
	_ = closeDB(oldReplica)

	logf(c.logger(), log.LevelInfo, "Connected to postgres")

	return nil
}

func (c *Client) buildConnection(ctx context.Context) (*sql.DB, *sql.DB, dbresolver.DB, error) {
	logf(c.logger(), log.LevelInfo, "Connecting to primary and replica databases...")

	primary, err := c.newSQLDB(c.cfg.PrimaryDSN)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("postgres connect: %w", err)
	}

	replica, err := c.newSQLDB(c.cfg.ReplicaDSN)
	if err != nil {
		_ = closeDB(primary)
		return nil, nil, nil, fmt.Errorf("postgres connect: %w", err)
	}

	resolver, err := createResolverFn(primary, replica)
	if err != nil {
		_ = closeDB(primary)
		_ = closeDB(replica)

		logf(c.logger(), log.LevelError, "failed to create resolver: %v", err)

		return nil, nil, nil, fmt.Errorf("postgres connect: failed to create resolver: %w", err)
	}

	if err := resolver.PingContext(ctx); err != nil {
		_ = resolver.Close()
		_ = closeDB(primary)
		_ = closeDB(replica)

		logf(c.logger(), log.LevelError, "failed to ping database: %v", err)

		return nil, nil, nil, fmt.Errorf("postgres connect: failed to ping database: %w", err)
	}

	return primary, replica, resolver, nil
}

func (c *Client) newSQLDB(dsn string) (*sql.DB, error) {
	db, err := dbOpenFn("pgx", dsn)
	if err != nil {
		sanitizedErr := sanitizeSensitiveError(err)
		logf(c.logger(), log.LevelError, "failed to open database: %s", sanitizedErr)

		return nil, fmt.Errorf("failed to open database: %s", sanitizedErr)
	}

	db.SetMaxOpenConns(c.cfg.MaxOpenConnections)
	db.SetMaxIdleConns(c.cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(defaultConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	return db, nil
}

// Resolver returns the resolver, connecting lazily if needed.
// Unlike sync.Once, this uses double-checked locking so that a transient
// failure on the first call does not permanently break the client --
// subsequent calls will retry the connection.
func (c *Client) Resolver(ctx context.Context) (dbresolver.DB, error) {
	if c == nil {
		return nil, fmt.Errorf("postgres resolver: %w", ErrNilClient)
	}

	if ctx == nil {
		return nil, fmt.Errorf("postgres resolver: %w", ErrNilContext)
	}

	// Fast path: already connected (read-lock only).
	c.mu.RLock()
	resolver := c.resolver
	c.mu.RUnlock()

	if resolver != nil {
		return resolver, nil
	}

	// Slow path: acquire write lock and double-check before connecting.
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.resolver != nil {
		return c.resolver, nil
	}

	if err := c.connectLocked(ctx); err != nil {
		return nil, err
	}

	if c.resolver == nil {
		return nil, fmt.Errorf("postgres resolver: %w", ErrNotConnected)
	}

	return c.resolver, nil
}

// Primary returns the current primary sql.DB, useful for admin operations.
func (c *Client) Primary() (*sql.DB, error) {
	if c == nil {
		return nil, fmt.Errorf("postgres primary: %w", ErrNilClient)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.primary == nil {
		return nil, fmt.Errorf("postgres primary: %w", ErrNotConnected)
	}

	return c.primary, nil
}

// Close releases database resources.
func (c *Client) Close() error {
	if c == nil {
		return fmt.Errorf("postgres close: %w", ErrNilClient)
	}

	c.mu.Lock()
	resolver := c.resolver
	primary := c.primary
	replica := c.replica

	c.resolver = nil
	c.primary = nil
	c.replica = nil
	c.mu.Unlock()

	if resolver != nil {
		if err := resolver.Close(); err != nil {
			return fmt.Errorf("postgres close: %w", err)
		}

		return nil
	}

	if err := closeDB(primary); err != nil {
		return fmt.Errorf("postgres close: %w", err)
	}

	if err := closeDB(replica); err != nil {
		return fmt.Errorf("postgres close: %w", err)
	}

	return nil
}

// IsConnected reports whether the resolver is currently initialized.
func (c *Client) IsConnected() (bool, error) {
	if c == nil {
		asserter := assert.New(context.Background(), nil, "postgres", "IsConnected")
		_ = asserter.Never(context.Background(), "postgres client receiver is nil")

		return false, ErrNilClient
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.resolver != nil, nil
}

func closeDB(db *sql.DB) error {
	if db == nil {
		return nil
	}

	return db.Close()
}

func logf(logger log.Logger, level log.Level, format string, args ...any) {
	if logger == nil {
		logger = log.NewNop()
	}

	logger.Log(context.Background(), level, fmt.Sprintf(format, args...))
}

// MigrationConfig stores migration-only settings.
type MigrationConfig struct {
	PrimaryDSN           string
	DatabaseName         string
	MigrationsPath       string
	Component            string
	AllowMultiStatements bool
	Logger               log.Logger
}

func (c MigrationConfig) withDefaults() MigrationConfig {
	if c.Logger == nil {
		c.Logger = log.NewNop()
	}

	return c
}

func (c MigrationConfig) validate() error {
	if strings.TrimSpace(c.PrimaryDSN) == "" {
		return fmt.Errorf("%w: primary dsn cannot be empty", ErrInvalidConfig)
	}

	if err := validateDBName(c.DatabaseName); err != nil {
		return err
	}

	if strings.TrimSpace(c.MigrationsPath) == "" && strings.TrimSpace(c.Component) == "" {
		return fmt.Errorf("%w: migrations_path or component is required", ErrInvalidConfig)
	}

	return nil
}

// Migrator runs schema migrations explicitly.
type Migrator struct {
	cfg MigrationConfig
}

// NewMigrator creates a migrator with explicit migration config.
func NewMigrator(cfg MigrationConfig) (*Migrator, error) {
	cfg = cfg.withDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("postgres new_migrator: %w", err)
	}

	return &Migrator{cfg: cfg}, nil
}

// Up runs all up migrations.
func (m *Migrator) Up(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("postgres migrate_up: %w", ErrNilClient)
	}

	if ctx == nil {
		return fmt.Errorf("postgres migrate_up: %w", ErrNilContext)
	}

	db, err := dbOpenFn("pgx", m.cfg.PrimaryDSN)
	if err != nil {
		sanitizedErr := sanitizeSensitiveError(err)
		logf(m.cfg.Logger, log.LevelError, "failed to open migration database: %s", sanitizedErr)

		return fmt.Errorf("postgres migrate_up: failed to open migration database: %s", sanitizedErr)
	}
	defer db.Close()

	migrationsPath, err := resolveMigrationsPath(m.cfg.MigrationsPath, m.cfg.Component)
	if err != nil {
		logf(m.cfg.Logger, log.LevelError, "failed to resolve migration path: %v", err)
		return fmt.Errorf("postgres migrate_up: %w", err)
	}

	if err := runMigrationsFn(db, migrationsPath, m.cfg.DatabaseName, m.cfg.AllowMultiStatements, m.cfg.Logger); err != nil {
		return fmt.Errorf("postgres migrate_up: %w", err)
	}

	return nil
}

func resolveMigrationsPath(migrationsPath, component string) (string, error) {
	if strings.TrimSpace(migrationsPath) != "" {
		return sanitizePath(migrationsPath)
	}

	// filepath.Base strips directory components, so "../../etc" becomes "etc".
	sanitized := filepath.Base(component)
	if sanitized == "." || sanitized == string(filepath.Separator) || sanitized == "" {
		return "", fmt.Errorf("invalid component name: %q", component)
	}

	calculatedPath, err := filepath.Abs(filepath.Join("components", sanitized, "migrations"))
	if err != nil {
		return "", err
	}

	return calculatedPath, nil
}

func sanitizeSensitiveError(err error) string {
	if err == nil {
		return ""
	}

	sanitized := connectionStringCredentialsPattern.ReplaceAllString(err.Error(), "://***@")
	sanitized = connectionStringPasswordPattern.ReplaceAllString(sanitized, "${1}***")

	return sanitized
}

func sanitizePath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	parts := strings.Split(cleaned, string(filepath.Separator))

	for _, part := range parts {
		if part == ".." {
			return "", fmt.Errorf("invalid migrations path: %q", path)
		}
	}

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to resolve migrations path: %w", err)
	}

	return absPath, nil
}

func validateDBName(name string) error {
	if !dbNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidDatabaseName, name)
	}

	return nil
}

func runMigrations(dbPrimary *sql.DB, migrationsPath, primaryDBName string, allowMultiStatements bool, logger log.Logger) error {
	if err := validateDBName(primaryDBName); err != nil {
		logf(logger, log.LevelError, "invalid primary database name: %v", err)
		return err
	}

	primaryURL, err := url.Parse(filepath.ToSlash(migrationsPath))
	if err != nil {
		logf(logger, log.LevelError, "failed to parse migrations url: %v", err)
		return fmt.Errorf("failed to parse migrations url: %w", err)
	}

	primaryURL.Scheme = "file"

	primaryDriver, err := postgres.WithInstance(dbPrimary, &postgres.Config{
		MultiStatementEnabled: allowMultiStatements,
		DatabaseName:          primaryDBName,
		SchemaName:            "public",
	})
	if err != nil {
		logf(logger, log.LevelError, "failed to create postgres driver instance: %v", err)
		return fmt.Errorf("failed to create postgres driver instance: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(primaryURL.String(), primaryDBName, primaryDriver)
	if err != nil {
		logf(logger, log.LevelError, "failed to get migrations: %v", err)
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logf(logger, log.LevelInfo, "No new migrations found. Skipping...")
			return nil
		}

		if errors.Is(err, os.ErrNotExist) {
			logf(logger, log.LevelWarn, "No migration files found. Skipping migration step...")
			return nil
		}

		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			logf(logger, log.LevelError, "Migration failed with dirty version %d", dirtyErr.Version)
			return fmt.Errorf("%w: database version %d", ErrMigrationDirty, dirtyErr.Version)
		}

		logf(logger, log.LevelError, "Migration failed: %v", err)

		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}
