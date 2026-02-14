package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/license"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/runtime"
	"github.com/gofiber/fiber/v2"
	"google.golang.org/grpc"
)

// ErrNoServersConfigured indicates no servers were configured for the manager
var ErrNoServersConfigured = errors.New("no servers configured: use WithHTTPServer() or WithGRPCServer()")

// ServerManager handles the graceful shutdown of multiple server types.
// It can manage HTTP servers, gRPC servers, or both simultaneously.
type ServerManager struct {
	httpServer         *fiber.App
	grpcServer         *grpc.Server
	licenseClient      *license.ManagerShutdown
	telemetry          *opentelemetry.Telemetry
	logger             log.Logger
	httpAddress        string
	grpcAddress        string
	serversStarted     chan struct{}
	serversStartedOnce sync.Once
	shutdownChan       <-chan struct{}
	shutdownOnce       sync.Once
	shutdownTimeout    time.Duration
	startupErrors      chan error
}

// NewServerManager creates a new instance of ServerManager.
// If logger is nil, a no-op logger is used to ensure nil-safe operation
// throughout the server lifecycle.
func NewServerManager(
	licenseClient *license.ManagerShutdown,
	telemetry *opentelemetry.Telemetry,
	logger log.Logger,
) *ServerManager {
	if logger == nil {
		logger = log.NewNop()
	}

	return &ServerManager{
		licenseClient:   licenseClient,
		telemetry:       telemetry,
		logger:          logger,
		serversStarted:  make(chan struct{}),
		shutdownTimeout: 30 * time.Second,
		startupErrors:   make(chan error, 2),
	}
}

// WithHTTPServer configures the HTTP server for the ServerManager.
func (sm *ServerManager) WithHTTPServer(app *fiber.App, address string) *ServerManager {
	sm.httpServer = app
	sm.httpAddress = address

	return sm
}

// WithGRPCServer configures the gRPC server for the ServerManager.
func (sm *ServerManager) WithGRPCServer(server *grpc.Server, address string) *ServerManager {
	sm.grpcServer = server
	sm.grpcAddress = address

	return sm
}

// WithShutdownChannel configures a custom shutdown channel for the ServerManager.
// This allows tests to trigger shutdown deterministically instead of relying on OS signals.
func (sm *ServerManager) WithShutdownChannel(ch <-chan struct{}) *ServerManager {
	sm.shutdownChan = ch

	return sm
}

// WithShutdownTimeout configures the maximum duration to wait for gRPC GracefulStop
// before forcing a hard stop. Defaults to 30 seconds.
func (sm *ServerManager) WithShutdownTimeout(d time.Duration) *ServerManager {
	sm.shutdownTimeout = d

	return sm
}

// ServersStarted returns a channel that is closed when server goroutines have been launched.
// Note: This signals that goroutines were spawned, not that sockets are bound and ready to accept connections.
// This is useful for tests to coordinate shutdown timing after server launch.
func (sm *ServerManager) ServersStarted() <-chan struct{} {
	return sm.serversStarted
}

func (sm *ServerManager) validateConfiguration() error {
	if sm.httpServer == nil && sm.grpcServer == nil {
		return ErrNoServersConfigured
	}

	return nil
}

// initServers validates configuration and starts servers without blocking.
// Returns an error if validation fails. Does not call Fatal.
func (sm *ServerManager) initServers() error {
	if sm.serversStarted == nil {
		sm.serversStarted = make(chan struct{})
	}

	if err := sm.validateConfiguration(); err != nil {
		return err
	}

	sm.startServers()

	return nil
}

// StartWithGracefulShutdownWithError validates configuration and starts servers.
// Returns an error if no servers are configured instead of calling Fatal.
// Blocks until shutdown signal is received or shutdown channel is closed.
func (sm *ServerManager) StartWithGracefulShutdownWithError() error {
	if err := sm.initServers(); err != nil {
		return err
	}

	sm.handleShutdown()

	return nil
}

// StartWithGracefulShutdown initializes all configured servers and sets up graceful shutdown.
// It terminates the process with os.Exit(1) if no servers are configured (backward compatible behavior).
// Note: On configuration error, logFatal always terminates the process regardless of logger availability.
// Use StartWithGracefulShutdownWithError() for proper error handling without process termination.
func (sm *ServerManager) StartWithGracefulShutdown() {
	if err := sm.initServers(); err != nil {
		// logFatal exits the process via os.Exit(1); code below is unreachable on error
		sm.logFatal(err.Error())
	}

	// Run everything in a recover block
	defer func() {
		if r := recover(); r != nil {
			runtime.HandlePanicValue(context.Background(), sm.logger, r, "server", "StartWithGracefulShutdown")

			sm.executeShutdown()

			os.Exit(1)
		}
	}()

	sm.handleShutdown()
}

// startServers starts all configured servers in separate goroutines.
// Note: Validation is performed by validateConfiguration() before this method is called.
// Callers using StartWithGracefulShutdown() directly will still get Fatal behavior for backward compatibility,
// while StartWithGracefulShutdownWithError() validates first and returns an error.
func (sm *ServerManager) startServers() {
	started := 0

	// Start HTTP server if configured
	if sm.httpServer != nil {
		runtime.SafeGoWithContextAndComponent(
			context.Background(),
			sm.logger,
			"server",
			"start_http_server",
			runtime.KeepRunning,
			func(_ context.Context) {
				sm.logInfof("Starting HTTP server on %s", sm.httpAddress)

				if err := sm.httpServer.Listen(sm.httpAddress); err != nil {
					sm.logErrorf("HTTP server error: %v", err)

					select {
					case sm.startupErrors <- fmt.Errorf("HTTP server: %w", err):
					default:
					}
				}
			},
		)

		started++
	}

	// Start gRPC server if configured
	if sm.grpcServer != nil {
		runtime.SafeGoWithContextAndComponent(
			context.Background(),
			sm.logger,
			"server",
			"start_grpc_server",
			runtime.KeepRunning,
			func(_ context.Context) {
				sm.logInfof("Starting gRPC server on %s", sm.grpcAddress)

				listener, err := net.Listen("tcp", sm.grpcAddress)
				if err != nil {
					sm.logErrorf("Failed to listen on gRPC address: %v", err)

					select {
					case sm.startupErrors <- fmt.Errorf("gRPC listen: %w", err):
					default:
					}

					return
				}

				if err := sm.grpcServer.Serve(listener); err != nil {
					sm.logErrorf("gRPC server error: %v", err)

					select {
					case sm.startupErrors <- fmt.Errorf("gRPC serve: %w", err):
					default:
					}
				}
			},
		)

		started++
	}

	sm.logInfof("Launched %d server goroutine(s)", started)

	// Signal that server goroutines have been launched (not that sockets are bound).
	sm.serversStartedOnce.Do(func() {
		close(sm.serversStarted)
	})
}

// logInfo safely logs an info message if logger is available
func (sm *ServerManager) logInfo(msg string) {
	if sm.logger != nil {
		sm.logger.Log(context.Background(), log.LevelInfo, msg)
	}
}

// logInfof safely logs a formatted info message if logger is available
func (sm *ServerManager) logInfof(format string, args ...any) {
	if sm.logger != nil {
		sm.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf(format, args...))
	}
}

// logErrorf safely logs an error message if logger is available
func (sm *ServerManager) logErrorf(format string, args ...any) {
	if sm.logger != nil {
		sm.logger.Log(context.Background(), log.LevelError, fmt.Sprintf(format, args...))
	}
}

// logFatal logs a fatal message and terminates the process with os.Exit(1).
// Uses Error level for logging to avoid relying on logger implementations
// that may or may not call os.Exit(1) in their Fatal method.
func (sm *ServerManager) logFatal(msg string) {
	if sm.logger != nil {
		sm.logger.Log(context.Background(), log.LevelError, msg)
	} else {
		fmt.Println(msg)
	}

	os.Exit(1)
}

// handleShutdown sets up signal handling and executes the shutdown sequence
// when a termination signal is received, when the shutdown channel is closed,
// or when a server startup error is detected.
func (sm *ServerManager) handleShutdown() {
	if sm.shutdownChan != nil {
		select {
		case <-sm.shutdownChan:
		case err := <-sm.startupErrors:
			sm.logErrorf("Server startup failed: %v", err)
		}
	} else {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		select {
		case <-c:
			signal.Stop(c)
		case err := <-sm.startupErrors:
			sm.logErrorf("Server startup failed: %v", err)
		}
	}

	sm.logInfo("Gracefully shutting down all servers...")

	sm.executeShutdown()
}

// executeShutdown performs the actual shutdown operations in the correct order for ServerManager.
// It is idempotent: multiple calls are safe, but only the first invocation executes the shutdown sequence.
func (sm *ServerManager) executeShutdown() {
	sm.shutdownOnce.Do(func() {
		// Use a non-blocking read to check if servers have started.
		// This prevents a deadlock if a panic occurs before startServers() completes.
		select {
		case <-sm.serversStarted:
			// Servers started, proceed with normal shutdown.
		default:
			// Servers did not start (or start was interrupted).
			sm.logInfo("Shutdown initiated before servers were fully started.")
		}

		// Shutdown the HTTP server if available
		if sm.httpServer != nil {
			sm.logInfo("Shutting down HTTP server...")

			if err := sm.httpServer.Shutdown(); err != nil {
				sm.logErrorf("Error during HTTP server shutdown: %v", err)
			}
		}

		// Shutdown telemetry BEFORE gRPC server to allow metrics export
		if sm.telemetry != nil {
			sm.logInfo("Shutting down telemetry...")
			sm.telemetry.ShutdownTelemetry()
		}

		// Shutdown the gRPC server if available with a timeout
		if sm.grpcServer != nil {
			sm.logInfo("Shutting down gRPC server...")

			done := make(chan struct{})

			go func() {
				sm.grpcServer.GracefulStop()
				close(done)
			}()

			select {
			case <-done:
				sm.logInfo("gRPC server stopped gracefully")
			case <-time.After(sm.shutdownTimeout):
				sm.logInfo("gRPC graceful stop timed out, forcing stop...")
				sm.grpcServer.Stop()
			}
		}

		// Sync logger if available
		if sm.logger != nil {
			sm.logInfo("Syncing logger...")

			if err := sm.logger.Sync(context.Background()); err != nil {
				sm.logErrorf("Failed to sync logger: %v", err)
			}
		}

		// Shutdown license background refresh if available
		if sm.licenseClient != nil {
			sm.logInfo("Shutting down license background refresh...")
			sm.licenseClient.Terminate("shutdown")
		}

		sm.logInfo("Graceful shutdown completed")
	})
}
