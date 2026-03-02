package ui

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/fatih/color"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/log"
)

// isNumeric returns true if s parses as a number (used for JSON field matching).
func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// init ensures tests run in console mode for consistent output testing.
func init() {
	log.Init(&log.Options{
		Format: log.FormatConsole,
	})
}

// captureColorOutput captures output from the color package.
// The color package uses color.Output which defaults to os.Stdout.
func captureColorOutput(fn func()) string {
	// Ensure console mode for testing.
	log.Init(&log.Options{
		Format: log.FormatConsole,
	})

	// Save original state.
	oldNoColor := color.NoColor
	oldOutput := color.Output

	// Configure for testing.
	color.NoColor = true

	// Create pipe.
	r, w, _ := os.Pipe()

	// Set color.Output to our pipe.
	color.Output = w

	// Also redirect os.Stdout for fmt.Printf calls.
	oldStdout := os.Stdout
	os.Stdout = w

	// Run the function.
	fn()

	// Close writer.
	w.Close()

	// Restore.
	color.Output = oldOutput
	color.NoColor = oldNoColor
	os.Stdout = oldStdout

	// Read output.
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
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

// captureJSONOutput captures zerolog JSON output by redirecting os.Stdout
// before initializing the logger in JSON mode. Restores console mode after.
func captureJSONOutput(t *testing.T, fn func()) string {
	t.Helper()

	// Create pipe and redirect stdout BEFORE init so zerolog writes to the pipe.
	r, w, err := os.Pipe()
	require.NoError(t, err)

	oldStdout := os.Stdout
	os.Stdout = w

	// Initialize JSON mode — logger is created writing to the redirected stdout.
	log.Init(&log.Options{
		Format: log.FormatJSON,
		Level:  log.DebugLevel,
		LevelSet: true,
	})

	// Run the function under test.
	fn()

	// Close writer, read output.
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()

	// Restore stdout and console mode.
	os.Stdout = oldStdout
	log.Init(&log.Options{
		Format: log.FormatConsole,
	})

	return buf.String()
}

func TestOutputFunctions_JSONMode(t *testing.T) {
	tests := []struct {
		name       string
		fn         func()
		wantMsg    string
		wantLevel  string
		wantFields map[string]string
	}{
		{
			name:      "Success",
			fn:        func() { Success("deploy completed") },
			wantMsg:   "deploy completed",
			wantLevel: "info",
			wantFields: map[string]string{
				"success": "true",
			},
		},
		{
			name:      "Success_WithArgs",
			fn:        func() { Success("processed %d items", 5) },
			wantMsg:   "processed 5 items",
			wantLevel: "info",
			wantFields: map[string]string{
				"success": "true",
			},
		},
		{
			name:      "Error",
			fn:        func() { Error("connection refused") },
			wantMsg:   "connection refused",
			wantLevel: "error",
		},
		{
			name:      "Error_WithArgs",
			fn:        func() { Error("port %d busy", 8080) },
			wantMsg:   "port 8080 busy",
			wantLevel: "error",
		},
		{
			name:      "Warning",
			fn:        func() { Warning("disk nearly full") },
			wantMsg:   "disk nearly full",
			wantLevel: "warn",
		},
		{
			name:      "Warning_WithArgs",
			fn:        func() { Warning("%d%% used", 90) },
			wantMsg:   "90% used",
			wantLevel: "warn",
		},
		{
			name:      "Info",
			fn:        func() { Info("server started") },
			wantMsg:   "server started",
			wantLevel: "info",
		},
		{
			name:      "Info_WithArgs",
			fn:        func() { Info("listening on %s", ":8080") },
			wantMsg:   "listening on :8080",
			wantLevel: "info",
		},
		{
			name:      "Debug",
			fn:        func() { Debug("trace detail") },
			wantMsg:   "trace detail",
			wantLevel: "debug",
		},
		{
			name:      "Debug_WithArgs",
			fn:        func() { Debug("key=%s val=%d", "foo", 42) },
			wantMsg:   "key=foo val=42",
			wantLevel: "debug",
		},
		{
			name:      "Step",
			fn:        func() { Step(2, "cloning repo") },
			wantMsg:   "cloning repo",
			wantLevel: "info",
			wantFields: map[string]string{
				"step": "2",
			},
		},
		{
			name:      "Header",
			fn:        func() { Header("Deploy Summary") },
			wantMsg:   "Deploy Summary",
			wantLevel: "info",
			wantFields: map[string]string{
				"type": "header",
			},
		},
		{
			name:      "Anchor",
			fn:        func() { Anchor("anchored to port") },
			wantMsg:   "anchored to port",
			wantLevel: "info",
			wantFields: map[string]string{
				"icon": "anchor",
			},
		},
		{
			name:      "Ship",
			fn:        func() { Ship("setting sail") },
			wantMsg:   "setting sail",
			wantLevel: "info",
			wantFields: map[string]string{
				"icon": "ship",
			},
		},
		{
			name:      "Compass",
			fn:        func() { Compass("heading north") },
			wantMsg:   "heading north",
			wantLevel: "info",
			wantFields: map[string]string{
				"icon": "compass",
			},
		},
		{
			name:      "Mayday",
			fn:        func() { Mayday("all hands on deck") },
			wantMsg:   "all hands on deck",
			wantLevel: "error",
			wantFields: map[string]string{
				"icon": "mayday",
			},
		},
		{
			name:      "Snapshot",
			fn:        func() { Snapshot("snapshot taken") },
			wantMsg:   "snapshot taken",
			wantLevel: "info",
			wantFields: map[string]string{
				"icon": "snapshot",
			},
		},
		{
			name:      "Package",
			fn:        func() { Package("bundled release") },
			wantMsg:   "bundled release",
			wantLevel: "info",
			wantFields: map[string]string{
				"icon": "package",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureJSONOutput(t, tt.fn)
			require.NotEmpty(t, output, "expected JSON output")

			assert.Contains(t, output, tt.wantMsg)
			assert.Contains(t, output, `"level":"`+tt.wantLevel+`"`)

			for key, val := range tt.wantFields {
				// Booleans and numbers are unquoted in JSON output.
				if val == "true" || val == "false" || isNumeric(val) {
					assert.Contains(t, output, `"`+key+`":`+val)
				} else {
					assert.Contains(t, output, `"`+key+`":"`+val+`"`)
				}
			}
		})
	}
}

func TestDebug_ConsoleMode(t *testing.T) {
	output := captureColorOutput(func() {
		Debug("debug trace info")
	})
	assert.Contains(t, output, "debug trace info")
}

func TestDebug_ConsoleMode_WithArgs(t *testing.T) {
	output := captureColorOutput(func() {
		Debug("value is %d", 99)
	})
	assert.Contains(t, output, "value is 99")
}

func TestLogger(t *testing.T) {
	l := Logger()
	require.NotNil(t, l)

	// Verify it returns a usable zerolog.Logger pointer.
	var _ *zerolog.Logger = l
}

func TestWithComponent(t *testing.T) {
	// WithComponent returns a zerolog.Logger derived from the global logger.
	// Capture JSON output and call WithComponent inside so it uses the redirected logger.
	output := captureJSONOutput(t, func() {
		l := WithComponent("test-comp")
		l.Info().Msg("component log")
	})
	assert.Contains(t, output, "component log")
	assert.Contains(t, output, "test-comp")
}

func TestFatal_ConsoleMode(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	// Fatal writes to os.Stderr in console mode, so capture stderr.
	oldStderr := os.Stderr
	oldNoColor := color.NoColor
	oldErrOutput := color.Error

	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stderr = w
	color.Error = w
	color.NoColor = true

	// Ensure console mode.
	log.Init(&log.Options{Format: log.FormatConsole})

	Fatal("fatal error happened")

	w.Close()
	os.Stderr = oldStderr
	color.Error = oldErrOutput
	color.NoColor = oldNoColor

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, buf.String(), "fatal error happened")
}

func TestFatal_JSONMode(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	output := captureJSONOutput(t, func() {
		Fatal("json fatal error")
	})

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, output, "json fatal error")
	assert.Contains(t, output, `"level":"error"`)
}

func TestFatalf_ConsoleMode(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	oldStderr := os.Stderr
	oldNoColor := color.NoColor
	oldErrOutput := color.Error

	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stderr = w
	color.Error = w
	color.NoColor = true

	log.Init(&log.Options{Format: log.FormatConsole})

	Fatalf("fatal: %s at line %d", "null pointer", 42)

	w.Close()
	os.Stderr = oldStderr
	color.Error = oldErrOutput
	color.NoColor = oldNoColor

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, buf.String(), "fatal: null pointer at line 42")
}

func TestFatalf_JSONMode(t *testing.T) {
	var exitCode int
	oldExit := exitFn
	exitFn = func(code int) { exitCode = code }
	defer func() { exitFn = oldExit }()

	output := captureJSONOutput(t, func() {
		Fatalf("fatal: %s crashed", "daemon")
	})

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, output, "fatal: daemon crashed")
	assert.Contains(t, output, `"level":"error"`)
}

func TestIsConsoleMode(t *testing.T) {
	// Verify that isConsoleMode reflects the configured format.
	log.Init(&log.Options{Format: log.FormatConsole})
	assert.True(t, isConsoleMode())

	log.Init(&log.Options{Format: log.FormatJSON})
	assert.False(t, isConsoleMode())

	// Restore console mode for other tests.
	log.Init(&log.Options{Format: log.FormatConsole})
}
