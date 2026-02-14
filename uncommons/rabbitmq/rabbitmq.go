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
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/backoff"
	constant "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	libOpentelemetry "github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry/metrics"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// connectionFailuresMetric defines the counter for rabbitmq connection failures.
var connectionFailuresMetric = metrics.Metric{
	Name:        "rabbitmq_connection_failures_total",
	Unit:        "1",
	Description: "Total number of rabbitmq connection failures",
}

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
	MetricsFactory         *metrics.MetricsFactory
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

	// Reconnect rate-limiting: prevents thundering-herd reconnect storms
	// when the broker is down by enforcing exponential backoff between attempts.
	lastReconnectAttempt time.Time
	reconnectAttempts    int
}

const defaultRabbitMQHealthCheckTimeout = 5 * time.Second

// reconnectBackoffCap is the maximum delay between reconnect attempts.
const reconnectBackoffCap = 30 * time.Second

// ErrInsecureTLS is returned when the health check HTTP client has TLS verification disabled
// without explicitly acknowledging the risk via AllowInsecureTLS.
var ErrInsecureTLS = errors.New("rabbitmq health check HTTP client has TLS verification disabled — set AllowInsecureTLS to acknowledge this risk")

// ErrNilConnection is returned when a method is called on a nil RabbitMQConnection.
var ErrNilConnection = errors.New("rabbitmq connection is nil")

// nilConnectionAssert fires a telemetry assertion for nil-receiver calls and returns ErrNilConnection.
// The logger is intentionally nil here because this function is called on a nil *RabbitMQConnection
// receiver, so there is no struct instance from which to extract a logger. The assert package
// handles nil loggers gracefully by falling back to stderr.
func nilConnectionAssert(operation string) error {
	asserter := assert.New(context.Background(), nil, "rabbitmq", operation)
	_ = asserter.Never(context.Background(), "rabbitmq connection receiver is nil")

	return ErrNilConnection
}

// Connect keeps a singleton connection with rabbitmq.
func (rc *RabbitMQConnection) Connect() error {
	return rc.ConnectContext(context.Background())
}

// ConnectContext keeps a singleton connection with rabbitmq.
func (rc *RabbitMQConnection) ConnectContext(ctx context.Context) error {
	if rc == nil {
		return nilConnectionAssert("connect_context")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rabbitmq connect: %w", err)
	}

	tracer := otel.Tracer("rabbitmq")

	ctx, span := tracer.Start(ctx, "rabbitmq.connect")
	defer span.End()

	span.SetAttributes(attribute.String(constant.AttrDBSystem, constant.DBSystemRabbitMQ))

	rc.mu.Lock()

	if err := rc.applyDefaults(); err != nil {
		rc.mu.Unlock()

		libOpentelemetry.HandleSpanError(span, "Failed to apply defaults", err)

		return fmt.Errorf("rabbitmq connect: %w", err)
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

	logger.Log(context.Background(), log.LevelInfo, "connecting to rabbitmq")

	conn, err := dialer(ctx, connStr)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, "failed to connect to rabbitmq", log.String("error_detail", sanitizeAMQPErr(err, connStr)))
		rc.recordConnectionFailure("connect")

		sanitizedErr := newSanitizedError(err, connStr, "failed to connect to rabbitmq")
		libOpentelemetry.HandleSpanError(span, "Failed to connect to rabbitmq", sanitizedErr)

		return sanitizedErr
	}

	ch, err := channelFactory(ctx, conn)
	if err != nil {
		rc.closeConnectionWith(conn, connCloser)

		logger.Log(context.Background(), log.LevelError, "failed to open channel on rabbitmq", log.Err(err))

		libOpentelemetry.HandleSpanError(span, "Failed to open channel on rabbitmq", err)

		return fmt.Errorf("failed to open channel on rabbitmq: %w", err)
	}

	if ch == nil || !rc.healthCheck(ctx, healthCheckURL, healthUser, healthPass, healthClient, logger) {
		rc.closeConnectionWith(conn, connCloser)

		err = errors.New("can't connect rabbitmq")

		logger.Log(context.Background(), log.LevelError, "rabbitmq health check failed")

		libOpentelemetry.HandleSpanError(span, "RabbitMQ health check failed", err)

		return fmt.Errorf("rabbitmq health check failed: %w", err)
	}

	logger.Log(context.Background(), log.LevelInfo, "connected to rabbitmq")

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

// ensureChannelSnapshot captures state needed by EnsureChannelContext under the lock.
type ensureChannelSnapshot struct {
	connStr            string
	logger             log.Logger
	dialer             func(context.Context, string) (*amqp.Connection, error)
	channelFactory     func(context.Context, *amqp.Connection) (*amqp.Channel, error)
	connCloser         func(*amqp.Connection) error
	connectionClosedFn func(*amqp.Connection) bool
	needConnection     bool
	needChannel        bool
	existingConn       *amqp.Connection
}

// snapshotEnsureChannelState captures and returns a snapshot of state needed for channel
// ensuring, applying defaults and rate-limiting under the lock. Returns an error if
// defaults fail or the request is rate-limited.
func (rc *RabbitMQConnection) snapshotEnsureChannelState() (ensureChannelSnapshot, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if err := rc.applyDefaults(); err != nil {
		return ensureChannelSnapshot{}, fmt.Errorf("rabbitmq ensure channel: %w", err)
	}

	connectionClosedFn := rc.connectionClosedFn
	channelClosedFn := rc.channelClosedFn
	needConnection := rc.Connection == nil || connectionClosedFn(rc.Connection)
	needChannel := needConnection || rc.Channel == nil || channelClosedFn(rc.Channel)

	// Rate-limit reconnect attempts: if we've failed recently, enforce a
	// minimum delay before the next attempt to prevent reconnect storms.
	if needConnection && rc.reconnectAttempts > 0 {
		delay := backoff.ExponentialWithJitter(500*time.Millisecond, rc.reconnectAttempts)
		if delay > reconnectBackoffCap {
			delay = reconnectBackoffCap
		}

		if elapsed := time.Since(rc.lastReconnectAttempt); elapsed < delay {
			return ensureChannelSnapshot{}, fmt.Errorf("rabbitmq ensure channel: rate-limited (next attempt in %s)", delay-elapsed)
		}
	}

	return ensureChannelSnapshot{
		connStr:            rc.ConnectionStringSource,
		logger:             rc.logger(),
		dialer:             rc.dialerContext,
		channelFactory:     rc.channelFactoryContext,
		connCloser:         rc.connectionCloser,
		connectionClosedFn: connectionClosedFn,
		needConnection:     needConnection,
		needChannel:        needChannel,
		existingConn:       rc.Connection,
	}, nil
}

// EnsureChannelContext ensures that the channel is open and connected.
func (rc *RabbitMQConnection) EnsureChannelContext(ctx context.Context) error {
	if rc == nil {
		return nilConnectionAssert("ensure_channel_context")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rabbitmq ensure channel: %w", err)
	}

	tracer := otel.Tracer("rabbitmq")

	ctx, span := tracer.Start(ctx, "rabbitmq.ensure_channel")
	defer span.End()

	span.SetAttributes(attribute.String(constant.AttrDBSystem, constant.DBSystemRabbitMQ))

	snap, err := rc.snapshotEnsureChannelState()
	if err != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to prepare ensure channel state", err)
		return err
	}

	if !snap.needChannel {
		return nil
	}

	var conn *amqp.Connection

	newConnection := false

	if snap.needConnection {
		rc.mu.Lock()
		rc.lastReconnectAttempt = time.Now()
		rc.mu.Unlock()

		conn, err = snap.dialer(ctx, snap.connStr)
		if err != nil {
			snap.logger.Log(context.Background(), log.LevelError, "failed to connect to rabbitmq", log.String("error_detail", sanitizeAMQPErr(err, snap.connStr)))
			rc.recordConnectionFailure("ensure_channel_connect")

			rc.mu.Lock()
			rc.Connected = false
			rc.reconnectAttempts++
			rc.mu.Unlock()

			sanitizedErr := newSanitizedError(err, snap.connStr, "can't connect to rabbitmq")
			libOpentelemetry.HandleSpanError(span, "Failed to connect to rabbitmq", sanitizedErr)

			return sanitizedErr
		}

		newConnection = true
	} else {
		conn = snap.existingConn
	}

	ch, err := snap.channelFactory(ctx, conn)
	if err == nil && ch == nil {
		err = errors.New("channel factory returned nil channel")
	}

	if err != nil {
		rc.handleChannelFailure(conn, snap.existingConn, newConnection, snap.connCloser)
		rc.recordConnectionFailure("ensure_channel")

		snap.logger.Log(context.Background(), log.LevelError, "failed to open channel on rabbitmq", log.Err(err))

		libOpentelemetry.HandleSpanError(span, "Failed to open channel on rabbitmq", err)

		return fmt.Errorf("rabbitmq ensure channel: %w", err)
	}

	rc.mu.Lock()
	if newConnection {
		rc.Connection = conn
		rc.reconnectAttempts = 0
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
		return nil, nilConnectionAssert("get_new_connect_context")
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
		rc.logger().Log(context.Background(), log.LevelError, "failed to ensure channel", log.Err(err))

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
		return false, nilConnectionAssert("health_check_context")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	tracer := otel.Tracer("rabbitmq")

	ctx, span := tracer.Start(ctx, "rabbitmq.health_check")
	defer span.End()

	span.SetAttributes(attribute.String(constant.AttrDBSystem, constant.DBSystemRabbitMQ))

	rc.mu.Lock()

	if err := rc.applyDefaults(); err != nil {
		rc.logger().Log(context.Background(), log.LevelWarn, "rabbitmq apply defaults warning in health check", log.Err(err))
	}

	healthURL := rc.HealthCheckURL
	user := rc.User
	pass := rc.Pass
	client := rc.healthHTTPClient
	logger := rc.logger()
	rc.mu.Unlock()

	if !rc.healthCheck(ctx, healthURL, user, pass, client, logger) {
		err := errors.New("rabbitmq health check failed")
		libOpentelemetry.HandleSpanError(span, "RabbitMQ health check failed", err)

		return false, err
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
		logger.Log(context.Background(), log.LevelError, "context canceled during rabbitmq health check", log.Err(err))

		return false
	}

	healthURL, err := validateHealthCheckURL(rawHealthURL)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, "invalid rabbitmq health check URL", log.Err(err))

		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, "failed to create rabbitmq health check request", log.Err(err))

		return false
	}

	req.SetBasicAuth(user, pass)

	if client == nil {
		client = &http.Client{Timeout: defaultRabbitMQHealthCheckTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, "failed to execute rabbitmq health check request", log.Err(err))

		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Log(context.Background(), log.LevelError, "rabbitmq health check failed", log.String("status", resp.Status))

		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		logger.Log(context.Background(), log.LevelError, "failed to read rabbitmq health check response", log.Err(err))

		return false
	}

	var result map[string]any

	err = json.Unmarshal(body, &result)
	if err != nil {
		logger.Log(context.Background(), log.LevelError, "failed to parse rabbitmq health check response", log.Err(err))

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
		rc.logger().Log(context.Background(), log.LevelWarn, "failed to close rabbitmq connection during cleanup", log.Err(err))
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
		return nilConnectionAssert("close_context")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rabbitmq close: %w", err)
	}

	tracer := otel.Tracer("rabbitmq")

	ctx, span := tracer.Start(ctx, "rabbitmq.close")
	defer span.End()

	span.SetAttributes(attribute.String(constant.AttrDBSystem, constant.DBSystemRabbitMQ))

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
			logger.Log(context.Background(), log.LevelWarn, "failed to close rabbitmq channel", log.Err(err))
		}
	}

	if connection != nil {
		if err := connCloser(ctx, connection); err != nil {
			if closeErr == nil {
				closeErr = fmt.Errorf("failed to close rabbitmq connection: %w", err)
			} else {
				closeErr = errors.Join(closeErr, fmt.Errorf("failed to close rabbitmq connection: %w", err))
			}

			logger.Log(context.Background(), log.LevelWarn, "failed to close rabbitmq connection", log.Err(err))
		}
	}

	if closeErr != nil {
		libOpentelemetry.HandleSpanError(span, "Failed to close rabbitmq", closeErr)
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

// recordConnectionFailure increments the rabbitmq connection failure counter.
// No-op when MetricsFactory is nil.
func (rc *RabbitMQConnection) recordConnectionFailure(operation string) {
	if rc == nil || rc.MetricsFactory == nil {
		return
	}

	counter, err := rc.MetricsFactory.Counter(connectionFailuresMetric)
	if err != nil {
		rc.logger().Log(context.Background(), log.LevelWarn, "failed to create rabbitmq metric counter", log.Err(err))
		return
	}

	err = counter.
		WithLabels(map[string]string{
			"operation": constant.SanitizeMetricLabel(operation),
		}).
		AddOne(context.Background())
	if err != nil {
		rc.logger().Log(context.Background(), log.LevelWarn, "failed to record rabbitmq metric", log.Err(err))
	}
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
