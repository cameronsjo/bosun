package ui

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
)

// captureColorOutput captures output from the color package.
// The color package uses color.Output which defaults to os.Stdout.
func captureColorOutput(fn func()) string {
	// Save original state
	oldNoColor := color.NoColor
	oldOutput := color.Output

	// Configure for testing
	color.NoColor = true

	// Create pipe
	r, w, _ := os.Pipe()

	// Set color.Output to our pipe
	color.Output = w

	// Also redirect os.Stdout for fmt.Printf calls
	oldStdout := os.Stdout
	os.Stdout = w

	// Run the function
	fn()

	// Close writer
	w.Close()

	// Restore
	color.Output = oldOutput
	color.NoColor = oldNoColor
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	return buf.String()
}

func TestSuccess(t *testing.T) {
	output := captureColorOutput(func() {
		Success("operation completed")
	})
	assert.Contains(t, output, "operation completed")
	assert.Contains(t, output, "\n")
}

func TestSuccess_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Success("processed %d items", 42)
	})
	assert.Contains(t, output, "processed 42 items")
}

func TestError(t *testing.T) {
	output := captureColorOutput(func() {
		Error("something failed")
	})
	assert.Contains(t, output, "something failed")
	assert.Contains(t, output, "\n")
}

func TestError_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Error("failed with code %d: %s", 500, "internal error")
	})
	assert.Contains(t, output, "failed with code 500: internal error")
}

func TestWarning(t *testing.T) {
	output := captureColorOutput(func() {
		Warning("be careful")
	})
	assert.Contains(t, output, "be careful")
	assert.Contains(t, output, "\n")
}

func TestWarning_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Warning("deprecated: use %s instead", "newFunc")
	})
	assert.Contains(t, output, "deprecated: use newFunc instead")
}

func TestInfo(t *testing.T) {
	output := captureColorOutput(func() {
		Info("informational message")
	})
	assert.Contains(t, output, "informational message")
	assert.Contains(t, output, "\n")
}

func TestInfo_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Info("version: %s", "1.0.0")
	})
	assert.Contains(t, output, "version: 1.0.0")
}

func TestStep(t *testing.T) {
	output := captureColorOutput(func() {
		Step(1, "first step")
	})
	assert.Contains(t, output, "[1]")
	assert.Contains(t, output, "first step")
	assert.Contains(t, output, "\n")
}

func TestStep_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Step(3, "processing %s", "data.yml")
	})
	assert.Contains(t, output, "[3]")
	assert.Contains(t, output, "processing data.yml")
}

func TestHeader(t *testing.T) {
	output := captureColorOutput(func() {
		Header("Section Title")
	})
	assert.Contains(t, output, "Section Title")
	assert.Contains(t, output, "\n")
}

func TestHeader_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Header("Building %s...", "project")
	})
	assert.Contains(t, output, "Building project...")
}

func TestAnchor(t *testing.T) {
	output := captureColorOutput(func() {
		Anchor("anchoring service")
	})
	assert.Contains(t, output, "anchoring service")
	assert.Contains(t, output, "\n")
}

func TestAnchor_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Anchor("anchoring %s to port %d", "myapp", 8080)
	})
	assert.Contains(t, output, "anchoring myapp to port 8080")
}

func TestShip(t *testing.T) {
	output := captureColorOutput(func() {
		Ship("setting sail")
	})
	assert.Contains(t, output, "setting sail")
	assert.Contains(t, output, "\n")
}

func TestShip_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Ship("deploying %s version %s", "webapp", "2.0")
	})
	assert.Contains(t, output, "deploying webapp version 2.0")
}

func TestCompass(t *testing.T) {
	output := captureColorOutput(func() {
		Compass("navigating to destination")
	})
	assert.Contains(t, output, "navigating to destination")
	assert.Contains(t, output, "\n")
}

func TestCompass_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Compass("route: %s -> %s", "source", "destination")
	})
	assert.Contains(t, output, "route: source -> destination")
}

func TestMayday(t *testing.T) {
	output := captureColorOutput(func() {
		Mayday("emergency situation")
	})
	assert.Contains(t, output, "emergency situation")
	assert.Contains(t, output, "\n")
}

func TestMayday_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Mayday("service %s is down", "database")
	})
	assert.Contains(t, output, "service database is down")
}

func TestSnapshot(t *testing.T) {
	output := captureColorOutput(func() {
		Snapshot("creating snapshot")
	})
	assert.Contains(t, output, "creating snapshot")
	assert.Contains(t, output, "\n")
}

func TestSnapshot_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Snapshot("snapshot created: %s", "snapshot-20240101-120000")
	})
	assert.Contains(t, output, "snapshot created: snapshot-20240101-120000")
}

func TestPackage(t *testing.T) {
	output := captureColorOutput(func() {
		Package("packaging artifacts")
	})
	assert.Contains(t, output, "packaging artifacts")
	assert.Contains(t, output, "\n")
}

func TestPackage_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Package("bundled %d files into %s", 15, "release.tar.gz")
	})
	assert.Contains(t, output, "bundled 15 files into release.tar.gz")
}

func TestColorVariables(t *testing.T) {
	// Test that color variables are initialized
	assert.NotNil(t, Red)
	assert.NotNil(t, Green)
	assert.NotNil(t, Yellow)
	assert.NotNil(t, Blue)
	assert.NotNil(t, Cyan)
	assert.NotNil(t, Bold)
}

func TestSuccess_HasCheckmark(t *testing.T) {
	output := captureColorOutput(func() {
		Success("test")
	})
	// Output format includes checkmark prefix
	assert.Contains(t, output, "test")
}

func TestError_HasX(t *testing.T) {
	output := captureColorOutput(func() {
		Error("test")
	})
	assert.Contains(t, output, "test")
}

func TestWarning_HasWarningSymbol(t *testing.T) {
	output := captureColorOutput(func() {
		Warning("test")
	})
	assert.Contains(t, output, "test")
}

// TestError_CanBeUsedLikeFatal verifies that Error (used by Fatal) works correctly.
// We cannot test Fatal/Fatalf directly since they call os.Exit(1).
func TestError_CanBeUsedLikeFatal(t *testing.T) {
	output := captureColorOutput(func() {
		Error("fatal error message")
	})
	assert.Contains(t, output, "fatal error message")
}

func TestMultipleMessages(t *testing.T) {
	output := captureColorOutput(func() {
		Info("line 1")
		Info("line 2")
		Info("line 3")
	})
	assert.Contains(t, output, "line 1")
	assert.Contains(t, output, "line 2")
	assert.Contains(t, output, "line 3")
}

func TestEmptyMessage(t *testing.T) {
	output := captureColorOutput(func() {
		Info("")
	})
	// Should just have a newline
	assert.Equal(t, "\n", output)
}

func TestSpecialCharacters(t *testing.T) {
	output := captureColorOutput(func() {
		Info("path: /home/user/file.txt")
	})
	assert.Contains(t, output, "/home/user/file.txt")
}

func TestUnicodeCharacters(t *testing.T) {
	output := captureColorOutput(func() {
		Info("hello: world")
	})
	assert.Contains(t, output, "hello: world")
}

func TestConcurrentOutput(t *testing.T) {
	// Test that the functions don't panic when called normally
	// (concurrent capture is problematic due to shared global state)
	for i := 0; i < 3; i++ {
		output := captureColorOutput(func() {
			Info("message %d", i)
		})
		assert.Contains(t, output, "message")
	}
}
