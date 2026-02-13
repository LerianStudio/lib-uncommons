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

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
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
	dbOpenFn = sql.Open

	createResolverFn = func(primaryDB, replicaDB *sql.DB) (_ dbresolver.DB, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
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

// PostgresConnection is a hub which deal with postgres connections.
type PostgresConnection struct {
	ConnectionStringPrimary string
	ConnectionStringReplica string
	PrimaryDBName           string
	Component               string
	MigrationsPath          string
	AllowMultiStatements    bool
	Logger                  log.Logger
	MaxOpenConnections      int
	MaxIdleConnections      int
	connectionDB            dbresolver.DB
	connected               bool
	mu                      sync.RWMutex
}

// initDefaults sets sane default values for zero-value fields.
func (pc *PostgresConnection) initDefaults() {
	if pc.Logger == nil {
		pc.Logger = &log.NoneLogger{}
	}

	if pc.MaxOpenConnections <= 0 {
		pc.MaxOpenConnections = defaultMaxOpenConns
	}

	if pc.MaxIdleConnections <= 0 {
		pc.MaxIdleConnections = defaultMaxIdleConns
	}
}

// Connect keeps a singleton connection with postgres.
func (pc *PostgresConnection) Connect(ctxs ...context.Context) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	ctx := contextFrom(ctxs...)

	return pc.connectLocked(ctx)
}

// connectLocked performs the actual connection. Caller must hold pc.mu write lock.
func (pc *PostgresConnection) connectLocked(ctx context.Context) error {
	ctx = contextFrom(ctx)

	pc.initDefaults()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context canceled before database connection: %w", err)
	}

	if pc.connectionDB != nil {
		if err := pc.closeLocked(); err != nil {
			pc.Logger.Warnf("failed to close previous connection before reconnect: %v", err)
		}
	}

	pc.Logger.Info("Connecting to primary and replica databases...")

	dbPrimary, err := dbOpenFn("pgx", pc.ConnectionStringPrimary)
	if err != nil {
		sanitizedErr := sanitizeSensitiveError(err)
		pc.Logger.Errorf("failed to connect to primary database: %s", sanitizedErr)

		return fmt.Errorf("failed to connect to primary database: %s", sanitizedErr)
	}

	// Ensure primary is cleaned up if anything downstream fails.
	var success bool

	defer func() {
		if !success {
			dbPrimary.Close()
		}
	}()

	dbPrimary.SetMaxOpenConns(pc.MaxOpenConnections)
	dbPrimary.SetMaxIdleConns(pc.MaxIdleConnections)
	dbPrimary.SetConnMaxLifetime(defaultConnMaxLifetime)
	dbPrimary.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	dbReadOnlyReplica, err := dbOpenFn("pgx", pc.ConnectionStringReplica)
	if err != nil {
		sanitizedErr := sanitizeSensitiveError(err)
		pc.Logger.Errorf("failed to connect to replica database: %s", sanitizedErr)

		return fmt.Errorf("failed to connect to replica database: %s", sanitizedErr)
	}

	// Ensure replica is cleaned up if anything downstream fails.
	defer func() {
		if !success {
			dbReadOnlyReplica.Close()
		}
	}()

	dbReadOnlyReplica.SetMaxOpenConns(pc.MaxOpenConnections)
	dbReadOnlyReplica.SetMaxIdleConns(pc.MaxIdleConnections)
	dbReadOnlyReplica.SetConnMaxLifetime(defaultConnMaxLifetime)
	dbReadOnlyReplica.SetConnMaxIdleTime(defaultConnMaxIdleTime)

	connectionDB, err := createResolverFn(dbPrimary, dbReadOnlyReplica)
	if err != nil {
		pc.Logger.Errorf("failed to create resolver: %v", err)
		return fmt.Errorf("failed to create resolver: %w", err)
	}

	migrationsPath, err := pc.getMigrationsPath()
	if err != nil {
		pc.Logger.Errorf("failed to resolve migration path: %v", err)
		return err
	}

	if err := runMigrationsFn(dbPrimary, migrationsPath, pc.PrimaryDBName, pc.AllowMultiStatements, pc.Logger); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context canceled before ping: %w", err)
	}

	if err := connectionDB.PingContext(ctx); err != nil {
		pc.Logger.Errorf("failed to ping database: %v", err)
		return fmt.Errorf("failed to ping database: %w", err)
	}

	pc.connected = true
	pc.connectionDB = connectionDB

	pc.Logger.Info("Connected to postgres")

	success = true

	return nil
}

// GetDB returns a pointer to the postgres connection, initializing it if necessary.
func (pc *PostgresConnection) GetDB(ctxs ...context.Context) (dbresolver.DB, error) {
	ctx := contextFrom(ctxs...)

	pc.mu.RLock()

	if pc.connectionDB != nil {
		db := pc.connectionDB
		pc.mu.RUnlock()

		return db, nil
	}

	pc.mu.RUnlock()

	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Double-check after acquiring write lock.
	if pc.connectionDB != nil {
		return pc.connectionDB, nil
	}

	if err := pc.connectLocked(ctx); err != nil {
		return nil, err
	}

	return pc.connectionDB, nil
}

// Close releases database connection resources.
func (pc *PostgresConnection) Close() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	return pc.closeLocked()
}

func (pc *PostgresConnection) closeLocked() error {
	if pc.connectionDB != nil {
		err := pc.connectionDB.Close()
		pc.connectionDB = nil
		pc.connected = false

		return err
	}

	return nil
}

// IsConnected reports whether the database resolver is initialized.
func (pc *PostgresConnection) IsConnected() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return pc.connected
}

// getMigrationsPath returns the path to migration files, calculating it if not explicitly provided.
func (pc *PostgresConnection) getMigrationsPath() (string, error) {
	if pc.MigrationsPath != "" {
		return sanitizePath(pc.MigrationsPath)
	}

	// Sanitize Component to prevent path traversal (CWE-22).
	// filepath.Base strips directory components, so "../../etc" becomes "etc".
	sanitized := filepath.Base(pc.Component)
	if sanitized == "." || sanitized == string(filepath.Separator) {
		return "", fmt.Errorf("invalid component name: %q", pc.Component)
	}

	calculatedPath, err := filepath.Abs(filepath.Join("components", sanitized, "migrations"))
	if err != nil {
		pc.Logger.Errorf("failed to get migration filepath: %v", err)

		return "", err
	}

	return calculatedPath, nil
}

func contextFrom(ctxs ...context.Context) context.Context {
	if len(ctxs) == 0 || ctxs[0] == nil {
		return context.Background()
	}

	return ctxs[0]
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
		return fmt.Errorf("invalid database name: %q", name)
	}

	return nil
}

func runMigrations(dbPrimary *sql.DB, migrationsPath, primaryDBName string, allowMultiStatements bool, logger log.Logger) error {
	if err := validateDBName(primaryDBName); err != nil {
		logger.Errorf("invalid primary database name: %v", err)
		return err
	}

	primaryURL, err := url.Parse(filepath.ToSlash(migrationsPath))
	if err != nil {
		logger.Errorf("failed to parse migrations url: %v", err)
		return fmt.Errorf("failed to parse migrations url: %w", err)
	}

	primaryURL.Scheme = "file"

	primaryDriver, err := postgres.WithInstance(dbPrimary, &postgres.Config{
		MultiStatementEnabled: allowMultiStatements,
		DatabaseName:          primaryDBName,
		SchemaName:            "public",
	})
	if err != nil {
		logger.Errorf("failed to create postgres driver instance: %v", err)
		return fmt.Errorf("failed to create postgres driver instance: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(primaryURL.String(), primaryDBName, primaryDriver)
	if err != nil {
		logger.Errorf("failed to get migrations: %v", err)
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logger.Info("No new migrations found. Skipping...")
			return nil
		}

		if errors.Is(err, os.ErrNotExist) {
			logger.Warn("No migration files found. Skipping migration step...")
			return nil
		}

		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			logger.Errorf("Migration failed with dirty version %d", dirtyErr.Version)
			return fmt.Errorf("migration failed: dirty database version %d", dirtyErr.Version)
		}

		logger.Errorf("Migration failed: %v", err)

		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}
