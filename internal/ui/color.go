// Package ui provides colored console output with a nautical theme.
package ui

import (
	"fmt"
	"os"

	"github.com/fatih/color"
)

var (
	// Colors
	Red    = color.New(color.FgRed)
	Green  = color.New(color.FgGreen)
	Yellow = color.New(color.FgYellow)
	Blue   = color.New(color.FgBlue)
	Cyan   = color.New(color.FgCyan)
	Bold   = color.New(color.Bold)
)

// Success prints a green success message with checkmark.
func Success(format string, args ...any) {
	Green.Printf("✓ "+format+"\n", args...)
}

// Error prints a red error message with X.
func Error(format string, args ...any) {
	Red.Printf("✗ "+format+"\n", args...)
}

// Warning prints a yellow warning message.
func Warning(format string, args ...any) {
	Yellow.Printf("⚠ "+format+"\n", args...)
}

// Info prints a blue info message.
func Info(format string, args ...any) {
	Blue.Printf(format+"\n", args...)
}

// Step prints a numbered step in cyan.
func Step(n int, format string, args ...any) {
	Cyan.Printf("[%d] ", n)
	fmt.Printf(format+"\n", args...)
}

// Header prints a bold header.
func Header(format string, args ...any) {
	Bold.Printf(format+"\n", args...)
}

// Nautical messages
func Anchor(format string, args ...any) {
	Blue.Printf("⚓ "+format+"\n", args...)
}

func Ship(format string, args ...any) {
	Green.Printf("🚢 "+format+"\n", args...)
}

func Compass(format string, args ...any) {
	Cyan.Printf("🧭 "+format+"\n", args...)
}

func Mayday(format string, args ...any) {
	Red.Printf("🆘 "+format+"\n", args...)
}

func Snapshot(format string, args ...any) {
	Blue.Printf("📸 "+format+"\n", args...)
}

func Package(format string, args ...any) {
	Green.Printf("📦 "+format+"\n", args...)
}

// Fatal prints an error to stderr and exits.
func Fatal(format string, args ...any) {
	Red.Fprintf(os.Stderr, "✗ "+format+"\n", args...)
	os.Exit(1)
}

// Fatalf prints a formatted error and exits.
func Fatalf(format string, args ...any) {
	Red.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
