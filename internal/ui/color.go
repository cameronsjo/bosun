// Package ui provides colored console output with a nautical theme.
// It wraps the structured logging package to provide a user-friendly CLI experience.
package ui

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/rs/zerolog"

	"github.com/cameronsjo/bosun/internal/log"
)

var (
	// Colors for direct use in formatting.
	Red    = color.New(color.FgRed)
	Green  = color.New(color.FgGreen)
	Yellow = color.New(color.FgYellow)
	Blue   = color.New(color.FgBlue)
	Cyan   = color.New(color.FgCyan)
	Gray   = color.New(color.FgHiBlack)
	Bold   = color.New(color.Bold)

	// exitFn is the function called to terminate the process. Replaceable in tests.
	exitFn = os.Exit
)

// SetExitFn replaces the exit function used by Fatal/Fatalf and returns the previous one.
// Intended for test use only — not goroutine-safe. Call from the test goroutine before
// exercising code that may call Fatal.
// Usage: `old := ui.SetExitFn(func(int) {}); t.Cleanup(func() { ui.SetExitFn(old) })`
func SetExitFn(fn func(int)) func(int) {
	old := exitFn
	exitFn = fn
	return old
}

// isConsoleMode returns true if we should use colored console output.
func isConsoleMode() bool {
	return log.GetFormat() == log.FormatConsole
}

// Success prints a green success message with checkmark.
// In JSON mode, logs as info level with success=true.
func Success(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Green.Printf("✓ %s\n", msg)
	} else {
		log.Info().Bool("success", true).Msg(msg)
	}
}

// Error prints a red error message with X.
// In JSON mode, logs as error level.
func Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Red.Printf("✗ %s\n", msg)
	} else {
		log.Error().Msg(msg)
	}
}

// Warning prints a yellow warning message.
// In JSON mode, logs as warn level.
func Warning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Yellow.Printf("⚠ %s\n", msg)
	} else {
		log.Warn().Msg(msg)
	}
}

// Info prints a blue info message.
// In JSON mode, logs as info level.
func Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Blue.Printf("%s\n", msg)
	} else {
		log.Info().Msg(msg)
	}
}

// Debug prints a debug message (only visible when debug level is enabled).
// In console mode, prints in gray.
func Debug(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Gray.Println(msg)
	} else {
		log.Debug().Msg(msg)
	}
}

// Step prints a numbered step in cyan.
func Step(n int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Cyan.Printf("[%d] ", n)
		fmt.Printf("%s\n", msg)
	} else {
		log.Info().Int("step", n).Msg(msg)
	}
}

// Header prints a bold header.
func Header(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Bold.Printf("%s\n", msg)
	} else {
		log.Info().Str("type", "header").Msg(msg)
	}
}

// Nautical themed messages.

// Anchor prints an anchor-themed message.
func Anchor(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Blue.Printf("⚓ %s\n", msg)
	} else {
		log.Info().Str("icon", "anchor").Msg(msg)
	}
}

// Ship prints a ship-themed message.
func Ship(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Green.Printf("🚢 %s\n", msg)
	} else {
		log.Info().Str("icon", "ship").Msg(msg)
	}
}

// Compass prints a compass-themed message.
func Compass(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Cyan.Printf("🧭 %s\n", msg)
	} else {
		log.Info().Str("icon", "compass").Msg(msg)
	}
}

// Mayday prints a mayday-themed error message.
func Mayday(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Red.Printf("🆘 %s\n", msg)
	} else {
		log.Error().Str("icon", "mayday").Msg(msg)
	}
}

// Snapshot prints a snapshot-themed message.
func Snapshot(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Blue.Printf("📸 %s\n", msg)
	} else {
		log.Info().Str("icon", "snapshot").Msg(msg)
	}
}

// Package prints a package-themed message.
func Package(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Green.Printf("📦 %s\n", msg)
	} else {
		log.Info().Str("icon", "package").Msg(msg)
	}
}

// Fatal prints an error to stderr and exits with code 1.
func Fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Red.Fprintf(os.Stderr, "✗ %s\n", msg)
	} else {
		log.Error().Msg(msg)
	}
	exitFn(1)
}

// Fatalf prints a formatted error and exits with code 1.
func Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isConsoleMode() {
		_, _ = Red.Fprintf(os.Stderr, "%s\n", msg)
	} else {
		log.Error().Msg(msg)
	}
	exitFn(1)
}

// Logger returns a zerolog.Logger for structured logging with additional fields.
// Use this when you need to add structured fields to a log entry.
func Logger() *zerolog.Logger {
	return log.Logger()
}

// WithComponent returns a logger with the component field set.
func WithComponent(component string) zerolog.Logger {
	return log.Component(component)
}
