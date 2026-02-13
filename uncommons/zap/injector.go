package zap

import (
	"fmt"
	"log"
	"os"
	"strings"

	clog "github.com/LerianStudio/lib-uncommons/uncommons/log"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// callerSkipFrames accounts for the two wrapper layers between the public
// API (e.g., Info) and the actual zap.SugaredLogger call:
//  1. ZapWithTraceLogger.Info (public method)
//  2. ZapWithTraceLogger.logWithHydration / logfWithHydration (internal helper)
const callerSkipFrames = 2

// LoggerConfig holds all configuration needed to initialize the logger.
// Use this to inject configuration for testability instead of relying on os.Getenv directly.
type LoggerConfig struct {
	// EnvName controls the base logger configuration.
	// Recognized values (case-insensitive): "production", "staging", "uat", "development", "local".
	// Unrecognized or empty values default to production config (fail-safe).
	EnvName string

	// LogLevel overrides the default log level derived from EnvName.
	// Accepts any level recognized by zapcore.Level.Set: "debug", "info", "warn", "error", "dpanic", "panic", "fatal".
	// Empty string means no override (uses the default for the environment).
	LogLevel string

	// OtelLibraryName is the library name passed to the OpenTelemetry zap bridge.
	// Empty string defaults to "unknown".
	OtelLibraryName string
}

// ConfigFromEnv builds a LoggerConfig by reading environment variables.
// This is the bridge between the environment and the injectable config.
func ConfigFromEnv() LoggerConfig {
	return LoggerConfig{
		EnvName:         os.Getenv("ENV_NAME"),
		LogLevel:        os.Getenv("LOG_LEVEL"),
		OtelLibraryName: os.Getenv("OTEL_LIBRARY_NAME"),
	}
}

// InitializeLoggerFromConfig initializes the logger from an explicit configuration.
// This is the testable constructor — all inputs are injectable, no direct env reads.
//
//nolint:ireturn
func InitializeLoggerFromConfig(cfg LoggerConfig) (clog.Logger, error) {
	var zapCfg zap.Config

	envName := strings.ToLower(strings.TrimSpace(cfg.EnvName))

	switch envName {
	case "production", "staging", "uat":
		zapCfg = zap.NewProductionConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "development", "local":
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	default:
		// Fail-safe: unrecognized or empty ENV_NAME defaults to production config
		// to avoid accidentally exposing debug-level logs in deployed environments.
		log.Printf("WARNING: ENV_NAME not set or unrecognized (%q), defaulting to production config", cfg.EnvName)

		zapCfg = zap.NewProductionConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	if cfg.LogLevel != "" {
		var lvl zapcore.Level
		if err := lvl.Set(cfg.LogLevel); err != nil {
			log.Printf("Invalid LOG_LEVEL, fallback to InfoLevel: %v", err)

			lvl = zapcore.InfoLevel
		}

		zapCfg.Level = zap.NewAtomicLevelAt(lvl)
	}

	zapCfg.DisableStacktrace = true

	otelLibName := cfg.OtelLibraryName
	if otelLibName == "" {
		otelLibName = "unknown"
		log.Printf("WARNING: OTEL_LIBRARY_NAME not set, defaulting to %q", otelLibName)
	}

	logger, err := zapCfg.Build(zap.AddCallerSkip(callerSkipFrames), zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapcore.NewTee(core, otelzap.NewCore(otelLibName))
	}))
	if err != nil {
		return nil, fmt.Errorf("can't initialize zap logger: %w", err)
	}

	sugarLogger := logger.Sugar()

	// Boot diagnostics: use stdlib log so these are always visible regardless of configured level.
	log.Printf("Log level is (%v)", zapCfg.Level)
	log.Printf("Logger is (%T)", sugarLogger)

	return &ZapWithTraceLogger{
		Logger: sugarLogger,
	}, nil
}

// InitializeLoggerWithError initializes our log layer from environment variables and returns it with error handling.
// This is a convenience wrapper around InitializeLoggerFromConfig + ConfigFromEnv.
//
//nolint:ireturn
func InitializeLoggerWithError() (clog.Logger, error) {
	return InitializeLoggerFromConfig(ConfigFromEnv())
}

// Deprecated: Use InitializeLoggerWithError for proper error handling.
//
// InitializeLogger initializes our log layer and returns it.
//
//nolint:ireturn
func InitializeLogger() clog.Logger {
	logger, err := InitializeLoggerWithError()
	if err != nil {
		log.Fatalf("%v", err)
	}

	return logger
}
