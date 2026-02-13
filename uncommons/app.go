package uncommons

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/LerianStudio/lib-uncommons/uncommons/assert"
	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/uncommons/runtime"
)

// ErrLoggerNil is returned when the Logger is nil and cannot proceed.
var ErrLoggerNil = errors.New("logger is nil")

var (
	ErrNilLauncher = errors.New("launcher is nil")
	ErrEmptyApp    = errors.New("app name is empty")
	ErrNilApp      = errors.New("app is nil")
)

// App represents an application that will run as a deployable component.
// It's an entrypoint at main.go.
// RedisRepository provides an interface for redis.
//
//go:generate mockgen --destination=app_mock.go --package=commons . App
type App interface {
	Run(launcher *Launcher) error
}

// LauncherOption defines a function option for Launcher.
type LauncherOption func(l *Launcher)

// WithLogger adds a log.Logger component to launcher.
func WithLogger(logger log.Logger) LauncherOption {
	return func(l *Launcher) {
		l.Logger = logger
	}
}

// RunApp start all process registered before to the launcher.
func RunApp(name string, app App) LauncherOption {
	return func(l *Launcher) {
		if err := l.Add(name, app); err != nil && l != nil && l.Logger != nil {
			l.Logger.Log(context.Background(), log.LevelError, fmt.Sprintf("Launcher add app error: %v", err))
		}
	}
}

// Launcher manages apps.
type Launcher struct {
	Logger  log.Logger
	apps    map[string]App
	wg      *sync.WaitGroup
	Verbose bool
}

// Add runs an application in a goroutine.
func (l *Launcher) Add(appName string, a App) error {
	if l == nil {
		asserter := assert.New(context.Background(), nil, "launcher", "Add")
		_ = asserter.Never(context.Background(), "launcher receiver is nil")

		return ErrNilLauncher
	}

	if l.apps == nil {
		l.apps = make(map[string]App)
	}

	if l.wg == nil {
		l.wg = new(sync.WaitGroup)
	}

	if strings.TrimSpace(appName) == "" {
		asserter := assert.New(context.Background(), l.Logger, "launcher", "Add")
		_ = asserter.Never(context.Background(), "app name must not be empty")

		return ErrEmptyApp
	}

	if a == nil {
		asserter := assert.New(context.Background(), l.Logger, "launcher", "Add")
		_ = asserter.Never(context.Background(), "app must not be nil", "app_name", appName)

		return ErrNilApp
	}

	l.apps[appName] = a

	return nil
}

// Run every application registered before with Run method.
// Maintains backward compatibility - logs error internally if Logger is nil.
// For explicit error handling, use RunWithError instead.
func (l *Launcher) Run() {
	if err := l.RunWithError(); err != nil {
		if l.Logger != nil {
			l.Logger.Log(context.Background(), log.LevelError, fmt.Sprintf("Launcher error: %v", err))
		}
	}
}

// RunWithError runs all applications and returns an error if Logger is nil.
// Use this method when you need explicit error handling for launcher initialization.
func (l *Launcher) RunWithError() error {
	if l.Logger == nil {
		return ErrLoggerNil
	}

	count := len(l.apps)
	l.wg.Add(count)

	l.Logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Starting %d app(s)", count))

	for name, app := range l.apps {
		nameCopy := name
		appCopy := app

		runtime.SafeGoWithContextAndComponent(
			context.Background(),
			l.Logger,
			"launcher",
			"run_app_"+nameCopy,
			runtime.KeepRunning,
			func(_ context.Context) {
				defer l.wg.Done()

				l.Logger.Log(context.Background(), log.LevelInfo, "--")
				l.Logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Launcher: App (%s) starting", nameCopy))

				if err := appCopy.Run(l); err != nil {
					l.Logger.Log(context.Background(), log.LevelError, fmt.Sprintf("Launcher: App (%s) error:", nameCopy))
					l.Logger.Log(context.Background(), log.LevelError, fmt.Sprintf("%s", err))
				}

				l.Logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Launcher: App (%s) finished", nameCopy))
			},
		)
	}

	l.wg.Wait()

	l.Logger.Log(context.Background(), log.LevelInfo, "Launcher: Terminated")

	return nil
}

// NewLauncher create an instance of Launch.
func NewLauncher(opts ...LauncherOption) *Launcher {
	l := &Launcher{
		apps:    make(map[string]App),
		wg:      new(sync.WaitGroup),
		Verbose: true,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}
