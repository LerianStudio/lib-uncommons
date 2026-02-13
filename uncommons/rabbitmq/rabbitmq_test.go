package rabbitmq

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
)

func TestRabbitMQConnection_Connect(t *testing.T) {
	t.Parallel()

	t.Run("context canceled before connect", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			Logger:                 &log.NoneLogger{},
			dialerContext: func(context.Context, string) (*amqp.Connection, error) {
				dialerCalls++

				return &amqp.Connection{}, nil
			},
		}

		err := conn.ConnectContext(ctx)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 0, dialerCalls)
	})

	t.Run("dial error", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			Logger:                 &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return nil, errors.New("dial failed")
			},
		}

		err := conn.Connect()

		assert.Error(t, err)
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Connection)
		assert.Nil(t, conn.Channel)
		assert.Equal(t, 1, dialerCalls)
		assert.ErrorContains(t, err, "dial failed")
	})

	t.Run("channel error closes connection", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0
		closeCalls := 0

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			Logger:                 &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return &amqp.Connection{}, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return nil, errors.New("channel failed")
			},
			connectionCloser: func(*amqp.Connection) error {
				closeCalls++

				return nil
			},
		}

		err := conn.Connect()

		assert.Error(t, err)
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Connection)
		assert.Nil(t, conn.Channel)
		assert.Equal(t, 1, dialerCalls)
		assert.Equal(t, 1, closeCalls)
	})

	t.Run("health check failure resets connection", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0
		closeCalls := 0

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"error"}`))
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			HealthCheckURL:         healthServer.URL,
			Logger:                 &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return &amqp.Connection{}, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return &amqp.Channel{}, nil
			},
			connectionCloser: func(conn *amqp.Connection) error {
				closeCalls++

				return nil
			},
		}

		err := conn.Connect()

		assert.Error(t, err)
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Connection)
		assert.Nil(t, conn.Channel)
		assert.Equal(t, 1, dialerCalls)
		assert.Equal(t, 1, closeCalls)
	})

	t.Run("healthy server creates connection", func(t *testing.T) {
		dialerCalls := 0
		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"status":"ok"}`))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			HealthCheckURL:         healthServer.URL,
			Logger:                 &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return &amqp.Connection{}, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return false },
			channelClosedFn:    func(*amqp.Channel) bool { return false },
		}

		err := conn.Connect()

		assert.NoError(t, err)
		assert.True(t, conn.Connected)
		assert.NotNil(t, conn.Connection)
		assert.NotNil(t, conn.Channel)
		assert.Equal(t, 1, dialerCalls)
	})

	t.Run("does not hold lock while running health check", func(t *testing.T) {
		healthStarted := make(chan struct{})
		continueHealth := make(chan struct{})
		dialerCalls := int32(0)

		var once sync.Once
		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(healthStarted) })

			<-continueHealth

			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"status":"ok"}`))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			HealthCheckURL:         healthServer.URL,
			Logger:                 &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				atomic.AddInt32(&dialerCalls, 1)

				return &amqp.Connection{}, nil
			},
			connectionCloser: func(*amqp.Connection) error {
				return nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return false },
			channelClosedFn:    func(*amqp.Channel) bool { return false },
		}

		connectDone := make(chan error, 1)
		go func() {
			connectDone <- conn.Connect()
		}()

		<-healthStarted

		ensureDone := make(chan error, 1)
		go func() {
			ensureDone <- conn.EnsureChannel()
		}()

		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&dialerCalls) >= 2
		}, 200*time.Millisecond, 10*time.Millisecond)

		close(continueHealth)

		assert.NoError(t, <-connectDone)
		assert.NoError(t, <-ensureDone)
	})

	t.Run("nil logger is safe", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			ConnectionStringSource: "amqp://guest:guest@localhost:5672",
			dialer: func(string) (*amqp.Connection, error) {
				return nil, errors.New("dial failed")
			},
		}

		assert.NotPanics(t, func() {
			_ = conn.Connect()
		})
	})
}

func TestRabbitMQConnection_EnsureChannel(t *testing.T) {
	t.Parallel()

	t.Run("creates connection and channel when missing", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0
		channelCalls := 0

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return &amqp.Connection{}, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				channelCalls++

				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(connection *amqp.Connection) bool { return connection == nil },
			channelClosedFn:    func(ch *amqp.Channel) bool { return ch == nil },
		}

		err := conn.EnsureChannel()

		assert.NoError(t, err)
		assert.True(t, conn.Connected)
		assert.NotNil(t, conn.Connection)
		assert.NotNil(t, conn.Channel)
		assert.Equal(t, 1, dialerCalls)
		assert.Equal(t, 1, channelCalls)
	})

	t.Run("reuses open connection and channel", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0
		channelCalls := 0

		conn := &RabbitMQConnection{
			Connection: &amqp.Connection{},
			Channel:    &amqp.Channel{},
			Connected:  true,
			Logger:     &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return nil, errors.New("should not be called")
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				channelCalls++

				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return false },
			channelClosedFn:    func(*amqp.Channel) bool { return false },
		}

		err := conn.EnsureChannel()

		assert.NoError(t, err)
		assert.True(t, conn.Connected)
		assert.Equal(t, 0, dialerCalls)
		assert.Equal(t, 0, channelCalls)
	})

	t.Run("reopens channel when closed", func(t *testing.T) {
		t.Parallel()

		channelCalls := 0

		conn := &RabbitMQConnection{
			Connection: &amqp.Connection{},
			Channel:    &amqp.Channel{},
			Logger:     &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				return nil, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				channelCalls++

				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return false },
			channelClosedFn:    func(ch *amqp.Channel) bool { return ch != nil },
		}

		err := conn.EnsureChannel()

		assert.NoError(t, err)
		assert.True(t, conn.Connected)
		assert.Equal(t, 1, channelCalls)
		assert.NotNil(t, conn.Channel)
	})

	t.Run("context canceled before ensure channel", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return &amqp.Connection{}, nil
			},
		}

		err := conn.EnsureChannelContext(ctx)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 0, dialerCalls)
	})

	t.Run("nil context defaults to background", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Connection:         &amqp.Connection{},
			Channel:            &amqp.Channel{},
			Connected:          true,
			Logger:             &log.NoneLogger{},
			connectionClosedFn: func(*amqp.Connection) bool { return false },
			channelClosedFn:    func(*amqp.Channel) bool { return false },
		}

		assert.NotPanics(t, func() {
			//nolint:staticcheck // intentionally passing nil context
			err := conn.EnsureChannelContext(nil)
			assert.NoError(t, err)
		})
	})

	t.Run("resets stale connection on channel failure", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0
		closeCalls := 0

		connection := &amqp.Connection{}
		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return connection, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return nil, errors.New("failed to open")
			},
			connectionCloser: func(*amqp.Connection) error {
				closeCalls++

				return nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return true },
			channelClosedFn:    func(*amqp.Channel) bool { return true },
		}

		err := conn.EnsureChannel()

		assert.Error(t, err)
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Connection)
		assert.Nil(t, conn.Channel)
		assert.Equal(t, 1, dialerCalls)
		assert.Equal(t, 1, closeCalls)
	})
}

func TestRabbitMQConnection_GetNewConnect(t *testing.T) {
	t.Parallel()

	t.Run("context canceled before connect", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := conn.GetNewConnectContext(ctx)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, got)
	})

	t.Run("creates channel when not connected", func(t *testing.T) {
		t.Parallel()

		dialerCalls := int32(0)

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				atomic.AddInt32(&dialerCalls, 1)

				return &amqp.Connection{}, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(connection *amqp.Connection) bool { return connection == nil },
			channelClosedFn:    func(ch *amqp.Channel) bool { return ch == nil },
		}

		channel, err := conn.GetNewConnect()

		assert.NoError(t, err)
		assert.NotNil(t, channel)
		assert.Equal(t, int32(1), atomic.LoadInt32(&dialerCalls))
	})

	t.Run("reuses existing connected channel", func(t *testing.T) {
		t.Parallel()

		dialerCalls := 0
		channelCalls := 0

		existing := &amqp.Channel{}
		conn := &RabbitMQConnection{
			Connection: &amqp.Connection{},
			Channel:    existing,
			Connected:  true,
			Logger:     &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				dialerCalls++

				return nil, errors.New("should not be called")
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				channelCalls++

				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return false },
			channelClosedFn:    func(*amqp.Channel) bool { return false },
		}

		got, err := conn.GetNewConnect()

		assert.NoError(t, err)
		assert.Same(t, existing, got)
		assert.Equal(t, 0, dialerCalls)
		assert.Equal(t, 0, channelCalls)
	})

	t.Run("stale channel state returns error", func(t *testing.T) {
		t.Parallel()

		connection := &amqp.Connection{}
		closeCalls := 0
		conn := &RabbitMQConnection{
			Connection: connection,
			Channel:    nil,
			Connected:  true,
			Logger:     &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				return connection, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return nil, nil
			},
			connectionClosedFn: func(*amqp.Connection) bool { return true },
			channelClosedFn:    func(*amqp.Channel) bool { return true },
			connectionCloser: func(*amqp.Connection) error {
				closeCalls++

				return nil
			},
		}

		got, err := conn.GetNewConnect()

		assert.Error(t, err)
		assert.Nil(t, got)
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Connection)
		assert.Nil(t, conn.Channel)
		assert.Equal(t, 1, closeCalls)
	})

	t.Run("concurrent callers all succeed", func(t *testing.T) {
		dialerCalls := int32(0)

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			dialer: func(string) (*amqp.Connection, error) {
				atomic.AddInt32(&dialerCalls, 1)

				return &amqp.Connection{}, nil
			},
			channelFactory: func(*amqp.Connection) (*amqp.Channel, error) {
				return &amqp.Channel{}, nil
			},
			connectionClosedFn: func(connection *amqp.Connection) bool { return connection == nil },
			channelClosedFn:    func(ch *amqp.Channel) bool { return ch == nil },
		}

		const total = 10
		results := make(chan error, total)

		var wg sync.WaitGroup
		wg.Add(total)
		for i := 0; i < total; i++ {
			go func() {
				defer wg.Done()

				_, err := conn.GetNewConnect()
				results <- err
			}()
		}

		wg.Wait()
		close(results)

		for err := range results {
			assert.NoError(t, err)
		}

		// EnsureChannelContext releases the lock before dialing (to avoid holding it
		// during I/O). Under contention, a small number of goroutines may race to dial
		// before the first one finishes and updates the shared connection state. This is
		// the expected trade-off — rare duplicate dials vs. convoy effect.
		dials := atomic.LoadInt32(&dialerCalls)
		assert.GreaterOrEqual(t, dials, int32(1))
		assert.LessOrEqual(t, dials, int32(total))
		assert.True(t, conn.Connected)
		assert.NotNil(t, conn.Channel)
	})
}

func TestRabbitMQConnection_HealthCheck(t *testing.T) {
	t.Parallel()

	t.Run("healthy response", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"status":"ok"}`))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{
			HealthCheckURL: healthServer.URL,
			Logger:         &log.NoneLogger{},
		}

		assert.True(t, conn.HealthCheck())
	})

	t.Run("server returns error status", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte("err"))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{HealthCheckURL: healthServer.URL, Logger: &log.NoneLogger{}}

		assert.False(t, conn.HealthCheck())
	})

	t.Run("unhealthy response body", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"status":"error"}`))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{HealthCheckURL: healthServer.URL, Logger: &log.NoneLogger{}}

		assert.False(t, conn.HealthCheck())
	})

	t.Run("malformed response", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"status":`))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{HealthCheckURL: healthServer.URL, Logger: &log.NoneLogger{}}

		assert.False(t, conn.HealthCheck())
	})

	t.Run("null response", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte("null"))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		conn := &RabbitMQConnection{HealthCheckURL: healthServer.URL, Logger: &log.NoneLogger{}}

		assert.False(t, conn.HealthCheck())
	})

	t.Run("invalid URL returns false", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{HealthCheckURL: "http://[::1", Logger: &log.NoneLogger{}}

		assert.False(t, conn.HealthCheck())
	})

	t.Run("invalid URL scheme is rejected", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{HealthCheckURL: "ftp://localhost:15672", Logger: &log.NoneLogger{}}

		assert.False(t, conn.HealthCheck())
	})

	t.Run("context canceled before health check request", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer healthServer.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		conn := &RabbitMQConnection{
			HealthCheckURL: healthServer.URL,
			Logger:         &log.NoneLogger{},
		}

		assert.False(t, conn.HealthCheckContext(ctx))
	})

	t.Run("authentication", func(t *testing.T) {
		t.Parallel()

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok || username != "correct" || password != "correct" {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"status":"ok"}`))
			assert.NoError(t, err)
		}))
		defer healthServer.Close()

		badAuth := &RabbitMQConnection{
			HealthCheckURL: healthServer.URL,
			User:           "wrong",
			Pass:           "wrong",
			Logger:         &log.NoneLogger{},
		}

		goodAuth := &RabbitMQConnection{
			HealthCheckURL: healthServer.URL,
			User:           "correct",
			Pass:           "correct",
			Logger:         &log.NoneLogger{},
		}

		assert.False(t, badAuth.HealthCheck())
		assert.True(t, goodAuth.HealthCheck())
	})
}

func TestApplyDefaults_InsecureTLS(t *testing.T) {
	t.Parallel()

	t.Run("returns error when injected client disables TLS verification", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			healthHTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true, //nolint:gosec // intentional for test
					},
				},
			},
		}

		conn.mu.Lock()
		err := conn.applyDefaults()
		conn.mu.Unlock()

		assert.ErrorIs(t, err, ErrInsecureTLS)
	})

	t.Run("AllowInsecureTLS bypasses the check", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			healthHTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true, //nolint:gosec // intentional for test
					},
				},
			},
			AllowInsecureTLS: true,
		}

		conn.mu.Lock()
		err := conn.applyDefaults()
		conn.mu.Unlock()

		assert.NoError(t, err)
	})

	t.Run("no error for default client", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
		}

		conn.mu.Lock()
		err := conn.applyDefaults()
		conn.mu.Unlock()

		assert.NoError(t, err)
	})

	t.Run("no error for secure custom client", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Logger: &log.NoneLogger{},
			healthHTTPClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						MinVersion: tls.VersionTLS12,
					},
				},
			},
		}

		conn.mu.Lock()
		err := conn.applyDefaults()
		conn.mu.Unlock()

		assert.NoError(t, err)
	})
}

func TestValidateHealthCheckURL(t *testing.T) {
	t.Parallel()

	t.Run("trims spaces and appends health path", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			HealthCheckURL: "  http://localhost:15672  ",
			Logger:         &log.NoneLogger{},
		}

		normalized, err := validateHealthCheckURL(conn.HealthCheckURL)

		assert.NoError(t, err)
		assert.Equal(t, "http://localhost:15672/api/health/checks/alarms", normalized)
	})

	t.Run("preserves nested path and appends health endpoint", func(t *testing.T) {
		t.Parallel()

		normalized, err := validateHealthCheckURL("http://localhost:15672/custom/alerts")

		assert.NoError(t, err)
		assert.Equal(t, "http://localhost:15672/custom/alerts/api/health/checks/alarms", normalized)
	})

	t.Run("normalizes path with trailing slash", func(t *testing.T) {
		t.Parallel()

		normalized, err := validateHealthCheckURL("http://localhost:15672/custom/alerts/")

		assert.NoError(t, err)
		assert.Equal(t, "http://localhost:15672/custom/alerts/api/health/checks/alarms", normalized)
	})

	t.Run("requires host", func(t *testing.T) {
		t.Parallel()

		normalized, err := validateHealthCheckURL("http:///api/health")

		assert.Error(t, err)
		assert.Empty(t, normalized)
	})

	t.Run("rejects unsupported scheme", func(t *testing.T) {
		t.Parallel()

		normalized, err := validateHealthCheckURL("ftp://localhost:15672")

		assert.Error(t, err)
		assert.Empty(t, normalized)
	})

	t.Run("rejects user credentials", func(t *testing.T) {
		t.Parallel()

		normalized, err := validateHealthCheckURL("http://user:pass@localhost:15672")

		assert.Error(t, err)
		assert.Empty(t, normalized)
	})
}

func TestRabbitMQConnection_HealthCheck_UsesConfiguredPath(t *testing.T) {
	t.Parallel()

	gotPath := make(chan string, 1)

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"ok"}`))
		assert.NoError(t, err)
	}))
	defer healthServer.Close()

	conn := &RabbitMQConnection{
		HealthCheckURL: healthServer.URL + "/custom/alerts",
		Logger:         &log.NoneLogger{},
	}

	assert.True(t, conn.HealthCheck())

	select {
	case p := <-gotPath:
		assert.Equal(t, "/custom/alerts/api/health/checks/alarms", p)
	case <-time.After(1 * time.Second):
		t.Fatal("health check did not reach test server")
	}
}

func TestBuildRabbitMQConnectionString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		protocol string
		user     string
		pass     string
		host     string
		port     string
		vhost    string
		expected string
	}{
		{
			name:     "empty vhost",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "localhost",
			port:     "5672",
			expected: "amqp://guest:guest@localhost:5672",
		},
		{
			name:     "custom vhost",
			protocol: "amqp",
			user:     "admin",
			pass:     "secret",
			host:     "rabbitmq.example.com",
			port:     "5672",
			vhost:    "production",
			expected: "amqp://admin:secret@rabbitmq.example.com:5672/production",
		},
		{
			name:     "root vhost",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "localhost",
			port:     "5672",
			vhost:    "/",
			expected: "amqp://guest:guest@localhost:5672/%2F",
		},
		{
			name:     "vhost with spaces",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "localhost",
			port:     "5672",
			vhost:    "my vhost",
			expected: "amqp://guest:guest@localhost:5672/my%20vhost",
		},
		{
			name:     "vhost with slash",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "localhost",
			port:     "5672",
			vhost:    "env/prod/region1",
			expected: "amqp://guest:guest@localhost:5672/env%2Fprod%2Fregion1",
		},
		{
			name:     "vhost with hash and ampersand",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "localhost",
			port:     "5672",
			vhost:    "test#1&2",
			expected: "amqp://guest:guest@localhost:5672/test%231%262",
		},
		{
			name:     "password with special chars",
			protocol: "amqp",
			user:     "admin",
			pass:     "p@ss:word/123",
			host:     "localhost",
			port:     "5672",
			vhost:    "production",
			expected: "amqp://admin:p%40ss%3Aword%2F123@localhost:5672/production",
		},
		{
			name:     "username with special chars",
			protocol: "amqp",
			user:     "admin@domain:user",
			pass:     "secret",
			host:     "localhost",
			port:     "5672",
			vhost:    "production",
			expected: "amqp://admin%40domain%3Auser:secret@localhost:5672/production",
		},
		{
			name:     "ipv6 with port",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "::1",
			port:     "5672",
			expected: "amqp://guest:guest@[::1]:5672",
		},
		{
			name:     "ipv6 without port",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "::1",
			expected: "amqp://guest:guest@[::1]",
		},
		{
			name:     "hostname without port",
			protocol: "amqp",
			user:     "guest",
			pass:     "guest",
			host:     "rabbitmq.local",
			expected: "amqp://guest:guest@rabbitmq.local",
		},
		{
			name:     "empty credentials",
			protocol: "amqp",
			host:     "localhost",
			port:     "5672",
			expected: "amqp://localhost:5672",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := BuildRabbitMQConnectionString(tt.protocol, tt.user, tt.pass, tt.host, tt.port, tt.vhost)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeAMQPErr(t *testing.T) {
	t.Parallel()

	t.Run("redacts credentials from connection string in error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("dial tcp: lookup amqp://admin:s3cretP@ss@broker:5672")
		connectionString := "amqp://admin:s3cretP@ss@broker:5672"

		got := sanitizeAMQPErr(err, connectionString)

		assert.NotContains(t, got, "s3cretP@ss")
		assert.Contains(t, got, "xxxxx")
	})

	t.Run("nil error returns empty string", func(t *testing.T) {
		t.Parallel()

		got := sanitizeAMQPErr(nil, "amqp://guest:guest@localhost:5672")

		assert.Equal(t, "", got)
	})

	t.Run("unparseable connection string returns raw error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("something went wrong")

		got := sanitizeAMQPErr(err, "://not-a-url")

		assert.Equal(t, "something went wrong", got)
	})

	t.Run("error without connection string returns original message", func(t *testing.T) {
		t.Parallel()

		err := errors.New("timeout connecting to broker")

		got := sanitizeAMQPErr(err, "amqp://admin:secret@broker:5672")

		assert.Equal(t, "timeout connecting to broker", got)
		assert.NotContains(t, got, "secret")
	})

	t.Run("redacts decoded password when embedded standalone in error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("authentication failed: password=s3cr3t")
		connectionString := "amqp://admin:s3cr3t@broker:5672"

		got := sanitizeAMQPErr(err, connectionString)

		assert.NotContains(t, got, "s3cr3t")
		assert.Contains(t, got, "xxxxx")
	})

	t.Run("redacts URL-encoded password in decoded form", func(t *testing.T) {
		t.Parallel()

		// Password with special chars: p@ss:word/123 → encoded as p%40ss%3Aword%2F123
		err := errors.New("auth error for p@ss:word/123")
		connectionString := "amqp://admin:p%40ss%3Aword%2F123@broker:5672"

		got := sanitizeAMQPErr(err, connectionString)

		assert.NotContains(t, got, "p@ss:word/123")
		assert.Contains(t, got, "xxxxx")
	})

	t.Run("empty connection string returns raw error", func(t *testing.T) {
		t.Parallel()

		err := errors.New("something failed")

		got := sanitizeAMQPErr(err, "")

		assert.Equal(t, "something failed", got)
	})
}

func TestRabbitMQConnection_Close(t *testing.T) {
	t.Parallel()

	t.Run("close releases resources", func(t *testing.T) {
		t.Parallel()

		channelCloseCalls := int32(0)
		connectionCloseCalls := int32(0)

		conn := &RabbitMQConnection{
			Connection: &amqp.Connection{},
			Channel:    &amqp.Channel{},
			Connected:  true,
			channelCloser: func(*amqp.Channel) error {
				atomic.AddInt32(&channelCloseCalls, 1)

				return nil
			},
			connectionCloser: func(*amqp.Connection) error {
				atomic.AddInt32(&connectionCloseCalls, 1)

				return nil
			},
			Logger: &log.NoneLogger{},
		}

		err := conn.Close()

		assert.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&channelCloseCalls))
		assert.Equal(t, int32(1), atomic.LoadInt32(&connectionCloseCalls))
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Channel)
		assert.Nil(t, conn.Connection)
	})

	t.Run("close aggregates channel and connection errors", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Connection: &amqp.Connection{},
			Channel:    &amqp.Channel{},
			Connected:  true,
			channelCloser: func(*amqp.Channel) error {
				return errors.New("channel close failed")
			},
			connectionCloser: func(*amqp.Connection) error {
				return errors.New("connection close failed")
			},
			Logger: &log.NoneLogger{},
		}

		err := conn.Close()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "channel close failed")
		assert.Contains(t, err.Error(), "connection close failed")
		assert.False(t, conn.Connected)
		assert.Nil(t, conn.Channel)
		assert.Nil(t, conn.Connection)
	})

	t.Run("close only connection error", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{
			Connection: &amqp.Connection{},
			Channel:    &amqp.Channel{},
			Connected:  true,
			channelCloser: func(*amqp.Channel) error {
				return nil
			},
			connectionCloser: func(*amqp.Connection) error {
				return errors.New("connection close failed")
			},
			Logger: &log.NoneLogger{},
		}

		err := conn.Close()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection close failed")
	})

	t.Run("close on nil receiver is safe", func(t *testing.T) {
		t.Parallel()

		var rc *RabbitMQConnection

		assert.NotPanics(t, func() {
			err := rc.CloseContext(context.Background())
			assert.NoError(t, err)
		})
	})

	t.Run("close context canceled", func(t *testing.T) {
		t.Parallel()

		conn := &RabbitMQConnection{}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := conn.CloseContext(ctx)

		assert.ErrorIs(t, err, context.Canceled)
	})
}
