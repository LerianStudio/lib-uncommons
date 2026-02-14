package zap

import (
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const callerSkipFrames = 1

// Environment controls the baseline logger profile.
type Environment string

const (
	// EnvironmentProduction enables production-safe logging defaults.
	EnvironmentProduction Environment = "production"
	// EnvironmentStaging enables staging-safe logging defaults.
	EnvironmentStaging Environment = "staging"
	// EnvironmentUAT enables UAT-safe logging defaults.
	EnvironmentUAT Environment = "uat"
	// EnvironmentDevelopment enables verbose development logging defaults.
	EnvironmentDevelopment Environment = "development"
	// EnvironmentLocal enables verbose local-development logging defaults.
	EnvironmentLocal Environment = "local"
)

// Config contains all required logger initialization inputs.
type Config struct {
	Environment     Environment
	Level           string
	OTelLibraryName string
}

func (c Config) validate() error {
	if c.OTelLibraryName == "" {
		return errors.New("OTelLibraryName is required")
	}

	switch c.Environment {
	case EnvironmentProduction, EnvironmentStaging, EnvironmentUAT, EnvironmentDevelopment, EnvironmentLocal:
		return nil
	default:
		return fmt.Errorf("invalid environment %q", c.Environment)
	}
}

// New creates a structured logger from the given configuration.
//
// The returned Logger implements log.Logger and stores the runtime-adjustable
// level handle internally. Use Logger.Level() to access it.
func New(cfg Config) (*Logger, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid zap config: %w", err)
	}

	baseConfig := buildConfigByEnvironment(cfg.Environment)

	level, err := resolveLevel(cfg)
	if err != nil {
		return nil, err
	}

	baseConfig.Level = level
	baseConfig.DisableStacktrace = true

	coreOptions := []zap.Option{
		zap.AddCallerSkip(callerSkipFrames),
		zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, otelzap.NewCore(cfg.OTelLibraryName))
		}),
	}

	built, err := baseConfig.Build(coreOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return &Logger{logger: built, atomicLevel: level}, nil
}

func resolveLevel(cfg Config) (zap.AtomicLevel, error) {
	if strings.TrimSpace(cfg.Level) != "" {
		var parsed zapcore.Level
		if err := parsed.Set(cfg.Level); err != nil {
			return zap.AtomicLevel{}, fmt.Errorf("invalid level %q: %w", cfg.Level, err)
		}

		return zap.NewAtomicLevelAt(parsed), nil
	}

	if cfg.Environment == EnvironmentDevelopment || cfg.Environment == EnvironmentLocal {
		return zap.NewAtomicLevelAt(zapcore.DebugLevel), nil
	}

	return zap.NewAtomicLevelAt(zapcore.InfoLevel), nil
}

func buildConfigByEnvironment(environment Environment) zap.Config {
	if environment == EnvironmentDevelopment || environment == EnvironmentLocal {
		cfg := zap.NewDevelopmentConfig()
		cfg.Encoding = "json"
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

		return cfg
	}

	cfg := zap.NewProductionConfig()
	cfg.Encoding = "json"
	cfg.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	return cfg
}
