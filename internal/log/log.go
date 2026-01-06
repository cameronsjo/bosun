// Package log provides structured logging for bosun using zerolog.
// It supports both human-readable console output for CLI commands
// and JSON output for daemon mode and log aggregation.
package log

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/term"
)

// Format specifies the log output format.
type Format string

const (
	// FormatAuto automatically detects the best format based on environment.
	FormatAuto Format = "auto"
	// FormatConsole outputs human-readable colored logs.
	FormatConsole Format = "console"
	// FormatJSON outputs structured JSON logs.
	FormatJSON Format = "json"
)

// Level wraps zerolog.Level for external use.
type Level = zerolog.Level

// Log levels.
const (
	DebugLevel = zerolog.DebugLevel
	InfoLevel  = zerolog.InfoLevel
	WarnLevel  = zerolog.WarnLevel
	ErrorLevel = zerolog.ErrorLevel
)

// Global logger instance.
var logger zerolog.Logger

// config holds the current logging configuration.
var config struct {
	format Format
	level  Level
}

func init() {
	// Initialize with sensible defaults.
	// This ensures logging works even if Init() is not called.
	config.format = FormatAuto
	config.level = InfoLevel
	initLogger()
}

// Options configures the logger.
type Options struct {
	Format   Format
	Level    Level
	LevelSet bool // Explicitly set level (needed because Level 0 is TraceLevel)
	Output   io.Writer
}

// Init initializes the global logger with the given options.
// If options is nil, environment variables and auto-detection are used.
func Init(opts *Options) {
	if opts == nil {
		opts = &Options{}
	}

	// Apply format (priority: explicit > env > auto).
	if opts.Format != "" {
		config.format = opts.Format
	} else if envFormat := os.Getenv("BOSUN_LOG_FORMAT"); envFormat != "" {
		config.format = Format(strings.ToLower(envFormat))
	} else {
		config.format = FormatAuto
	}

	// Apply level (priority: explicit > env > default).
	if opts.LevelSet {
		config.level = opts.Level
	} else if envLevel := os.Getenv("BOSUN_LOG_LEVEL"); envLevel != "" {
		config.level = parseLevel(envLevel)
	} else {
		config.level = InfoLevel
	}

	initLogger()
}

// initLogger creates the logger based on current config.
func initLogger() {
	format := config.format
	if format == FormatAuto {
		format = detectFormat()
	}

	var output io.Writer = os.Stdout

	switch format {
	case FormatConsole:
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.Kitchen,
			FormatLevel: func(i interface{}) string {
				level := strings.ToUpper(i.(string))
				switch level {
				case "INFO":
					return "\033[34mINFO\033[0m" // Blue
				case "WARN":
					return "\033[33mWARN\033[0m" // Yellow
				case "ERROR":
					return "\033[31mERRO\033[0m" // Red
				case "DEBUG":
					return "\033[90mDEBG\033[0m" // Gray
				default:
					return level
				}
			},
		}
	case FormatJSON:
		// JSON output uses default zerolog behavior.
		output = os.Stdout
	}

	zerolog.SetGlobalLevel(config.level)
	logger = zerolog.New(output).With().Timestamp().Logger()
}

// detectFormat determines the best format based on environment.
func detectFormat() Format {
	// Check if running in daemon mode.
	if os.Getenv("BOSUN_DAEMON_MODE") == "true" {
		return FormatJSON
	}

	// Check if stdout is a terminal.
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return FormatConsole
	}

	// Default to JSON for non-terminal (pipes, files, containers).
	return FormatJSON
}

// parseLevel converts a string to a log level.
func parseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn", "warning":
		return WarnLevel
	case "error":
		return ErrorLevel
	default:
		return InfoLevel
	}
}

// Logger returns the global logger.
func Logger() *zerolog.Logger {
	return &logger
}

// Debug starts a debug-level log event.
func Debug() *zerolog.Event {
	return logger.Debug()
}

// Info starts an info-level log event.
func Info() *zerolog.Event {
	return logger.Info()
}

// Warn starts a warn-level log event.
func Warn() *zerolog.Event {
	return logger.Warn()
}

// Error starts an error-level log event.
func Error() *zerolog.Event {
	return logger.Error()
}

// Fatal starts a fatal-level log event.
// After logging, the process will exit with status 1.
func Fatal() *zerolog.Event {
	return logger.Fatal()
}

// With creates a child logger with additional context.
func With() zerolog.Context {
	return logger.With()
}

// SetGlobalLevel sets the minimum log level.
func SetGlobalLevel(level Level) {
	zerolog.SetGlobalLevel(level)
}

// GetFormat returns the current log format.
func GetFormat() Format {
	if config.format == FormatAuto {
		return detectFormat()
	}
	return config.format
}

// GetLevel returns the current log level.
func GetLevel() Level {
	return config.level
}
