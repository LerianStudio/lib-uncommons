package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/assert"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQConnection is a hub which deal with rabbitmq connections.
type RabbitMQConnection struct {
	mu                     sync.Mutex // protects connection and channel operations
	ConnectionStringSource string     `json:"-"`
	Connection             *amqp.Connection
	Queue                  string
	HealthCheckURL         string
	Host                   string
	Port                   string
	User                   string `json:"-"`
	Pass                   string `json:"-"`
	VHost                  string
	Channel                *amqp.Channel
	Logger                 log.Logger
	Connected              bool

	dialer                  func(string) (*amqp.Connection, error)
	dialerContext           func(context.Context, string) (*amqp.Connection, error)
	channelFactory          func(*amqp.Connection) (*amqp.Channel, error)
	channelFactoryContext   func(context.Context, *amqp.Connection) (*amqp.Channel, error)
	connectionCloser        func(*amqp.Connection) error
	connectionCloserContext func(context.Context, *amqp.Connection) error
	connectionClosedFn      func(*amqp.Connection) bool
	channelClosedFn         func(*amqp.Channel) bool
	channelCloser           func(*amqp.Channel) error
	channelCloserContext    func(context.Context, *amqp.Channel) error
	healthHTTPClient        *http.Client

	// AllowInsecureTLS must be set to true to explicitly acknowledge that
	// the health check HTTP client has TLS certificate verification disabled.
	// Without this flag, applyDefaults returns ErrInsecureTLS.
	AllowInsecureTLS bool
}

const defaultRabbitMQHealthCheckTimeout = 5 * time.Second

// ErrInsecureTLS is returned when the health check HTTP client has TLS verification disabled
// without explicitly acknowledging the risk via AllowInsecureTLS.
var ErrInsecureTLS = errors.New("rabbitmq health check HTTP client has TLS verification disabled — set AllowInsecureTLS to acknowledge this risk")

var ErrNilConnection = errors.New("rabbitmq connection is nil")

// Connect keeps a singleton connection with rabbitmq.
func (rc *RabbitMQConnection) Connect() error {
	return rc.ConnectContext(context.Background())
}

// ConnectContext keeps a singleton connection with rabbitmq.
func (rc *RabbitMQConnection) ConnectContext(ctx context.Context) error {
	if rc == nil {
		asserter := assert.New(context.Background(), nil, "rabbitmq", "ConnectContext")
		_ = asserter.Never(context.Background(), "rabbitmq connection receiver is nil")

		return ErrNilConnection
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	rc.mu.Lock()

	if err := rc.applyDefaults(); err != nil {
		rc.mu.Unlock()

		return err
	}

	connStr := rc.ConnectionStringSource
	healthCheckURL := rc.HealthCheckURL
	healthUser := rc.User
	healthPass := rc.Pass
	healthClient := rc.healthHTTPClient
	dialer := rc.dialerContext
	channelFactory := rc.channelFactoryContext
	connectionClosedFn := rc.connectionClosedFn
	connCloser := rc.connectionCloser
	logger := rc.logger()
	rc.mu.Unlock()

	logger.Log(context.Background(), log.LevelInfo, "Connecting on rabbitmq...")

	conn, err := dialer(ctx, connStr)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("failed to connect to rabbitmq: %v", sanitizeAMQPErr(err, connStr)))

		return newSanitizedError(err, connStr, "failed to connect to rabbitmq")
	}

	ch, err := channelFactory(ctx, conn)
	if err != nil {
		rc.closeConnectionWith(conn, connCloser)

		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("failed to open channel on rabbitmq: %v", err))

		return fmt.Errorf("failed to open channel on rabbitmq: %w", err)
	}

	if ch == nil || !rc.healthCheck(ctx, healthCheckURL, healthUser, healthPass, healthClient, logger) {
		rc.closeConnectionWith(conn, connCloser)

		err = errors.New("can't connect rabbitmq")

		logger.Log(context.Background(), log.LevelError, "rabbitmq health check failed")

		return fmt.Errorf("rabbitmq health check failed: %w", err)
	}

	logger.Log(context.Background(), log.LevelInfo, "Connected on rabbitmq")

	rc.mu.Lock()
	if rc.Connection != nil && rc.Connection != conn && !connectionClosedFn(rc.Connection) {
		rc.mu.Unlock()

		rc.closeConnectionWith(conn, connCloser)

		return nil
	}

	rc.Connected = true
	rc.Connection = conn
	rc.Channel = ch
	rc.mu.Unlock()

	return nil
}

// EnsureChannel ensures that the channel is open and connected.
func (rc *RabbitMQConnection) EnsureChannel() error {
	return rc.EnsureChannelContext(context.Background())
}

// EnsureChannelContext ensures that the channel is open and connected.
func (rc *RabbitMQConnection) EnsureChannelContext(ctx context.Context) error {
	if rc == nil {
		asserter := assert.New(context.Background(), nil, "rabbitmq", "EnsureChannelContext")
		_ = asserter.Never(context.Background(), "rabbitmq connection receiver is nil")

		return ErrNilConnection
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Snapshot state under lock, then release before any I/O (same discipline as ConnectContext).
	rc.mu.Lock()

	if err := rc.applyDefaults(); err != nil {
		rc.mu.Unlock()

		return err
	}

	connStr := rc.ConnectionStringSource
	logger := rc.logger()
	dialer := rc.dialerContext
	channelFactory := rc.channelFactoryContext
	connCloser := rc.connectionCloser
	connectionClosedFn := rc.connectionClosedFn
	channelClosedFn := rc.channelClosedFn

	needConnection := rc.Connection == nil || connectionClosedFn(rc.Connection)
	needChannel := needConnection || rc.Channel == nil || channelClosedFn(rc.Channel)
	existingConn := rc.Connection
	rc.mu.Unlock()

	if !needChannel {
		return nil
	}

	var conn *amqp.Connection

	newConnection := false

	if needConnection {
		var err error

		conn, err = dialer(ctx, connStr)
		if err != nil {
			logger.Log(context.Background(), log.LevelError, fmt.Sprintf("can't connect to rabbitmq: %v", sanitizeAMQPErr(err, connStr)))

			rc.mu.Lock()
			rc.Connected = false
			rc.mu.Unlock()

			return newSanitizedError(err, connStr, "can't connect to rabbitmq")
		}

		newConnection = true
	} else {
		conn = existingConn
	}

	ch, err := channelFactory(ctx, conn)
	if err == nil && ch == nil {
		err = errors.New("channel factory returned nil channel")
	}

	if err != nil {
		rc.handleChannelFailure(conn, existingConn, newConnection, connCloser)

		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("can't open channel on rabbitmq: %v", err))

		return err
	}

	rc.mu.Lock()
	if newConnection {
		rc.Connection = conn
	}

	rc.Channel = ch
	rc.Connected = true
	rc.mu.Unlock()

	return nil
}

// GetNewConnect returns a pointer to the rabbitmq connection, initializing it if necessary.
func (rc *RabbitMQConnection) GetNewConnect() (*amqp.Channel, error) {
	return rc.GetNewConnectContext(context.Background())
}

// GetNewConnectContext returns a pointer to the rabbitmq connection, initializing it if necessary.
func (rc *RabbitMQConnection) GetNewConnectContext(ctx context.Context) (*amqp.Channel, error) {
	if rc == nil {
		asserter := assert.New(context.Background(), nil, "rabbitmq", "GetNewConnectContext")
		_ = asserter.Never(context.Background(), "rabbitmq connection receiver is nil")

		return nil, ErrNilConnection
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rc.mu.Lock()

	if err := rc.applyDefaults(); err != nil {
		rc.mu.Unlock()

		return nil, err
	}

	if rc.Connected && rc.Channel != nil && !rc.channelClosedFn(rc.Channel) {
		ch := rc.Channel
		rc.mu.Unlock()

		return ch, nil
	}
	rc.mu.Unlock()

	if err := rc.EnsureChannelContext(ctx); err != nil {
		rc.logger().Log(context.Background(), log.LevelError, fmt.Sprintf("failed to ensure channel: %v", err))

		return nil, err
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.Channel == nil {
		rc.Connected = false

		return nil, errors.New("rabbitmq channel not available")
	}

	return rc.Channel, nil
}

// HealthCheck rabbitmq when the server is started.
func (rc *RabbitMQConnection) HealthCheck() (bool, error) {
	return rc.HealthCheckContext(context.Background())
}

// HealthCheckContext rabbitmq when the server is started.
// It captures config fields under lock to avoid reading them during concurrent mutation.
func (rc *RabbitMQConnection) HealthCheckContext(ctx context.Context) (bool, error) {
	if rc == nil {
		asserter := assert.New(context.Background(), nil, "rabbitmq", "HealthCheckContext")
		_ = asserter.Never(context.Background(), "rabbitmq connection receiver is nil")

		return false, ErrNilConnection
	}

	if ctx == nil {
		ctx = context.Background()
	}

	rc.mu.Lock()
	_ = rc.applyDefaults()
	healthURL := rc.HealthCheckURL
	user := rc.User
	pass := rc.Pass
	client := rc.healthHTTPClient
	logger := rc.logger()
	rc.mu.Unlock()

	if !rc.healthCheck(ctx, healthURL, user, pass, client, logger) {
		return false, errors.New("rabbitmq health check failed")
	}

	return true, nil
}

// healthCheck is the internal implementation that operates on pre-captured config values,
// safe to call without holding the mutex.
func (rc *RabbitMQConnection) healthCheck(ctx context.Context, rawHealthURL, user, pass string, client *http.Client, logger log.Logger) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("context canceled while running rabbitmq health check: %v", err))

		return false
	}

	healthURL, err := validateHealthCheckURL(rawHealthURL)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("invalid rabbitmq health check url: %v", err))

		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("failed to make GET request for rabbitmq health check: %v", err))

		return false
	}

	req.SetBasicAuth(user, pass)

	if client == nil {
		client = &http.Client{Timeout: defaultRabbitMQHealthCheckTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("failed to execute rabbitmq health check request: %v", err))

		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("rabbitmq health check failed with status %q", resp.Status))

		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("failed to read rabbitmq health check response: %v", err))

		return false
	}

	var result map[string]any

	err = json.Unmarshal(body, &result)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, fmt.Sprintf("failed to parse rabbitmq health check response: %v", err))

		return false
	}

	if result == nil {
		logger.Log(context.Background(), log.LevelError, "rabbitmq health check response is empty or null")

		return false
	}

	if status, ok := result["status"].(string); ok && status == "ok" {
		return true
	}

	logger.Log(context.Background(), log.LevelError, "rabbitmq is not healthy")

	return false
}

func (rc *RabbitMQConnection) applyDefaults() error {
	rc.applyConnectionDefaults()
	rc.applyChannelDefaults()

	return rc.applyHealthDefaults()
}

func (rc *RabbitMQConnection) applyConnectionDefaults() {
	if rc.dialer == nil {
		rc.dialer = amqp.Dial
	}

	if rc.dialerContext == nil {
		rc.dialerContext = func(_ context.Context, connectionString string) (*amqp.Connection, error) {
			return rc.dialer(connectionString)
		}
	}

	if rc.connectionCloser == nil {
		rc.connectionCloser = func(connection *amqp.Connection) error {
			if connection == nil {
				return nil
			}

			return connection.Close()
		}
	}

	if rc.connectionCloserContext == nil {
		rc.connectionCloserContext = func(_ context.Context, connection *amqp.Connection) error {
			return rc.connectionCloser(connection)
		}
	}

	if rc.connectionClosedFn == nil {
		rc.connectionClosedFn = func(connection *amqp.Connection) bool {
			if connection == nil {
				return true
			}

			return connection.IsClosed()
		}
	}
}

func (rc *RabbitMQConnection) applyChannelDefaults() {
	if rc.channelFactory == nil {
		rc.channelFactory = func(connection *amqp.Connection) (*amqp.Channel, error) {
			if connection == nil {
				return nil, errors.New("cannot create channel: connection is nil")
			}

			return connection.Channel()
		}
	}

	if rc.channelFactoryContext == nil {
		rc.channelFactoryContext = func(_ context.Context, connection *amqp.Connection) (*amqp.Channel, error) {
			return rc.channelFactory(connection)
		}
	}

	if rc.channelClosedFn == nil {
		rc.channelClosedFn = func(ch *amqp.Channel) bool {
			if ch == nil {
				return true
			}

			return ch.IsClosed()
		}
	}

	if rc.channelCloser == nil {
		rc.channelCloser = func(ch *amqp.Channel) error {
			if ch == nil {
				return nil
			}

			return ch.Close()
		}
	}

	if rc.channelCloserContext == nil {
		rc.channelCloserContext = func(_ context.Context, ch *amqp.Channel) error {
			return rc.channelCloser(ch)
		}
	}
}

func (rc *RabbitMQConnection) applyHealthDefaults() error {
	if rc.healthHTTPClient == nil {
		rc.healthHTTPClient = &http.Client{Timeout: defaultRabbitMQHealthCheckTimeout}

		return nil
	}

	transport, ok := rc.healthHTTPClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		return nil
	}

	if transport.TLSClientConfig.InsecureSkipVerify && !rc.AllowInsecureTLS {
		return ErrInsecureTLS
	}

	return nil
}

func (rc *RabbitMQConnection) closeConnectionWith(connection *amqp.Connection, closer func(*amqp.Connection) error) {
	if closer == nil {
		return
	}

	if err := closer(connection); err != nil {
		rc.logger().Log(context.Background(), log.LevelWarn, fmt.Sprintf("failed to close rabbitmq connection during cleanup: %v", err))
	}
}

// handleChannelFailure cleans up after a failed channel creation in EnsureChannelContext.
// It conditionally closes the connection and resets the channel/connected state.
func (rc *RabbitMQConnection) handleChannelFailure(conn, existingConn *amqp.Connection, newConnection bool, connCloser func(*amqp.Connection) error) {
	if newConnection {
		rc.closeConnectionWith(conn, connCloser)
	}

	rc.mu.Lock()
	if newConnection && rc.Connection == existingConn {
		rc.Connection = nil
	}

	rc.Channel = nil
	rc.Connected = false
	rc.mu.Unlock()
}

// Close closes the rabbitmq channel and connection.
func (rc *RabbitMQConnection) Close() error {
	return rc.CloseContext(context.Background())
}

// CloseContext closes the rabbitmq channel and connection.
func (rc *RabbitMQConnection) CloseContext(ctx context.Context) error {
	if rc == nil {
		asserter := assert.New(context.Background(), nil, "rabbitmq", "CloseContext")
		_ = asserter.Never(context.Background(), "rabbitmq connection receiver is nil")

		return ErrNilConnection
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	rc.mu.Lock()
	_ = rc.applyDefaults() // Close must not fail due to TLS config — resources still need cleanup.
	channel := rc.Channel
	connection := rc.Connection
	chCloser := rc.channelCloserContext
	connCloser := rc.connectionCloserContext
	rc.Connection = nil
	rc.Channel = nil
	rc.Connected = false
	logger := rc.logger()
	rc.mu.Unlock()

	var closeErr error

	if channel != nil {
		if err := chCloser(ctx, channel); err != nil {
			closeErr = fmt.Errorf("failed to close rabbitmq channel: %w", err)
			logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("failed to close rabbitmq channel: %v", err))
		}
	}

	if connection != nil {
		if err := connCloser(ctx, connection); err != nil {
			if closeErr == nil {
				closeErr = fmt.Errorf("failed to close rabbitmq connection: %w", err)
			} else {
				closeErr = fmt.Errorf("%w; failed to close rabbitmq connection: %v", closeErr, err)
			}

			logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("failed to close rabbitmq connection: %v", err))
		}
	}

	return closeErr
}

func (rc *RabbitMQConnection) logger() log.Logger {
	if rc == nil || rc.Logger == nil {
		return &log.NopLogger{}
	}

	return rc.Logger
}

// validateHealthCheckURL validates the health check URL and appends the RabbitMQ health endpoint path
// if not already present. The HealthCheckURL should be the RabbitMQ management API base URL
// (e.g., "http://host:15672" or "https://host:15672"), NOT the full health endpoint.
// If the URL already ends with "/api/health/checks/alarms", it is returned as-is.
//
// Security note: this function does NOT restrict which hosts can be targeted.
// The HealthCheckURL is assumed to come from trusted configuration (env vars, config files).
// If your threat model requires protection against SSRF via configuration injection, consider
// adding an allowlist of permitted hosts or IP ranges at the application layer.
func validateHealthCheckURL(rawURL string) (string, error) {
	healthURL := strings.TrimSpace(rawURL)
	if healthURL == "" {
		return "", errors.New("rabbitmq health check URL is empty")
	}

	parsedURL, err := url.Parse(healthURL)
	if err != nil {
		return "", err
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", errors.New("rabbitmq health check URL must use http or https")
	}

	if parsedURL.Host == "" {
		return "", errors.New("rabbitmq health check URL must include a host")
	}

	if parsedURL.User != nil {
		return "", errors.New("rabbitmq health check URL must not include user credentials")
	}

	// Only append the health endpoint path if not already present.
	const healthPath = "/api/health/checks/alarms"

	normalized := strings.TrimSuffix(parsedURL.String(), "/")
	if strings.HasSuffix(normalized, healthPath) {
		return normalized, nil
	}

	return normalized + healthPath, nil
}

// sanitizedError wraps an original error with a redacted message.
// Error() returns the sanitized message; Unwrap() returns the original
// so that errors.Is / errors.As still work for programmatic inspection.
type sanitizedError struct {
	original error
	message  string
}

// Error returns the sanitized message.
func (e *sanitizedError) Error() string { return e.message }

// Unwrap returns the original wrapped error.
func (e *sanitizedError) Unwrap() error { return e.original }

// newSanitizedError wraps err with a human-readable prefix and redacted connection string.
func newSanitizedError(err error, connectionString, prefix string) error {
	return fmt.Errorf("%s: %w", prefix, &sanitizedError{
		original: err,
		message:  sanitizeAMQPErr(err, connectionString),
	})
}

func sanitizeAMQPErr(err error, connectionString string) string {
	if err == nil {
		return ""
	}

	if connectionString == "" {
		return err.Error()
	}

	referenceURL, parseErr := url.Parse(connectionString)
	if parseErr != nil {
		return err.Error()
	}

	redactedURL := referenceURL.Redacted()

	errMsg := err.Error()
	if strings.Contains(errMsg, connectionString) {
		errMsg = strings.ReplaceAll(errMsg, connectionString, redactedURL)
	}

	if strings.Contains(errMsg, referenceURL.String()) {
		errMsg = strings.ReplaceAll(errMsg, referenceURL.String(), redactedURL)
	}

	// Redact decoded password individually — covers cases where the error message
	// contains the password in decoded form (e.g., URL-encoded special characters).
	if referenceURL.User != nil {
		if pass, ok := referenceURL.User.Password(); ok && pass != "" {
			errMsg = strings.ReplaceAll(errMsg, pass, "xxxxx")
		}
	}

	return errMsg
}

// BuildRabbitMQConnectionString constructs an AMQP connection string.
// If vhost is empty, the default vhost "/" is used (no path in URL).
// Special characters in user, password, and vhost are URL-encoded automatically.
// Supports IPv6 hosts (e.g., "[::1]").
func BuildRabbitMQConnectionString(protocol, user, pass, host, port, vhost string) string {
	u := &url.URL{Scheme: protocol}
	if user != "" || pass != "" {
		u.User = url.UserPassword(user, pass)
	}

	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		// Bracket bare IPv6 addresses to avoid malformed URLs (e.g., amqp://user:pass@::1)
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			u.Host = "[" + host + "]"
		} else {
			u.Host = host
		}
	}

	if vhost != "" {
		// Use QueryEscape instead of PathEscape because RabbitMQ vhost names may contain '/'
		// which must be percent-encoded as %2F. QueryEscape encodes '/' while PathEscape does not.
		// The subsequent ReplaceAll converts query-style space encoding (+) to path-style (%20).
		escapedVHost := url.QueryEscape(vhost)
		escapedVHost = strings.ReplaceAll(escapedVHost, "+", "%20")
		u.Path = "/" + vhost
		u.RawPath = "/" + escapedVHost
	}

	return u.String()
}
