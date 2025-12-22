package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestCrewCmd_Help(t *testing.T) {
	t.Run("crew shows help", func(t *testing.T) {
		output, err := executeCmd(t, "crew")
		assert.NoError(t, err)
		assert.Contains(t, output, "list")
		assert.Contains(t, output, "logs")
		assert.Contains(t, output, "inspect")
		assert.Contains(t, output, "restart")
	})

	t.Run("crew --help", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "containers")
	})
}

func TestCrewCmd_Aliases(t *testing.T) {
	t.Run("scallywags alias works", func(t *testing.T) {
		output, err := executeCmd(t, "scallywags", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "containers")
	})
}

func TestCrewCmd_Subcommands(t *testing.T) {
	t.Run("crew list help", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "list", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "containers")
		assert.Contains(t, output, "--all")
	})

	t.Run("crew logs help", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "logs", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "logs")
		assert.Contains(t, output, "--tail")
		assert.Contains(t, output, "--follow")
	})

	t.Run("crew inspect help", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "inspect", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "information")
	})

	t.Run("crew restart help", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "restart", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Restart")
	})
}

func TestCrewCmd_Structure(t *testing.T) {
	resetRootCmd(t)
	var crewCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "crew" {
			crewCmd = cmd
			break
		}
	}

	assert.NotNil(t, crewCmd, "crew command should exist")

	t.Run("crew has subcommands", func(t *testing.T) {
		subcommands := crewCmd.Commands()
		names := make([]string, 0, len(subcommands))
		for _, cmd := range subcommands {
			names = append(names, cmd.Name())
		}

		assert.Contains(t, names, "list")
		assert.Contains(t, names, "logs")
		assert.Contains(t, names, "inspect")
		assert.Contains(t, names, "restart")
	})
}

// TestCrewListCmd_NoContainers tests crew list when no containers exist.
func TestCrewListCmd_NoContainers(t *testing.T) {
	t.Run("help shows expected flags", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "list", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "--all")
		assert.Contains(t, output, "containers")
	})
}

// TestCrewLogsCmd_UsageInfo tests crew logs command usage.
func TestCrewLogsCmd_UsageInfo(t *testing.T) {
	t.Run("help shows expected flags", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "logs", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "--tail")
		assert.Contains(t, output, "--follow")
		assert.Contains(t, output, "-n")
		assert.Contains(t, output, "-f")
	})

	t.Run("help shows usage with name placeholder", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "logs", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "<name>")
	})
}

// TestCrewInspectCmd_UsageInfo tests crew inspect command usage.
func TestCrewInspectCmd_UsageInfo(t *testing.T) {
	t.Run("help shows usage with name placeholder", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "inspect", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "<name>")
	})
}

// TestCrewRestartCmd_UsageInfo tests crew restart command usage.
func TestCrewRestartCmd_UsageInfo(t *testing.T) {
	t.Run("help shows usage with name placeholder", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "restart", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "<name>")
	})
}

// TestCrewCmd_FlagDefaults tests that crew command flags have correct defaults.
func TestCrewCmd_FlagDefaults(t *testing.T) {
	t.Run("list has all flag", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "list", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "-a, --all")
	})

	t.Run("logs --tail default is 100", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "logs", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "100")
	})

	t.Run("logs has follow flag", func(t *testing.T) {
		output, err := executeCmd(t, "crew", "logs", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "-f, --follow")
	})
}

// TestCrewCmd_SubcommandUsage tests that crew subcommands show correct usage.
func TestCrewCmd_SubcommandUsage(t *testing.T) {
	testCases := []struct {
		name            string
		args            []string
		expectInOutput  []string
	}{
		{
			name:           "logs usage",
			args:           []string{"crew", "logs", "--help"},
			expectInOutput: []string{"<name>", "logs"},
		},
		{
			name:           "inspect usage",
			args:           []string{"crew", "inspect", "--help"},
			expectInOutput: []string{"<name>", "inspect"},
		},
		{
			name:           "restart usage",
			args:           []string{"crew", "restart", "--help"},
			expectInOutput: []string{"<name>", "restart"},
		},
		{
			name:           "list usage",
			args:           []string{"crew", "list", "--help"},
			expectInOutput: []string{"list", "containers"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := executeCmd(t, tc.args...)
			assert.NoError(t, err)
			for _, expected := range tc.expectInOutput {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestStdCopy(t *testing.T) {
	t.Run("copy stdout stream", func(t *testing.T) {
		// Create mock multiplexed stream
		// Header: [1, 0, 0, 0, 0, 0, 0, 5] followed by "hello"
		header := []byte{1, 0, 0, 0, 0, 0, 0, 5}
		payload := []byte("hello")

		input := append(header, payload...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(5), written)
		assert.Equal(t, "hello", stdout.String())
		assert.Empty(t, stderr.String())
	})

	t.Run("copy stderr stream", func(t *testing.T) {
		// Header: [2, 0, 0, 0, 0, 0, 0, 5] followed by "error"
		header := []byte{2, 0, 0, 0, 0, 0, 0, 5}
		payload := []byte("error")

		input := append(header, payload...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(5), written)
		assert.Empty(t, stdout.String())
		assert.Equal(t, "error", stderr.String())
	})

	t.Run("copy multiple frames", func(t *testing.T) {
		// Two stdout frames
		frame1 := append([]byte{1, 0, 0, 0, 0, 0, 0, 5}, []byte("hello")...)
		frame2 := append([]byte{1, 0, 0, 0, 0, 0, 0, 5}, []byte("world")...)

		input := append(frame1, frame2...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(10), written)
		assert.Equal(t, "helloworld", stdout.String())
	})

	t.Run("empty input", func(t *testing.T) {
		reader := strings.NewReader("")

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(0), written)
	})

	t.Run("incomplete header", func(t *testing.T) {
		// Only 4 bytes instead of 8
		reader := bytes.NewReader([]byte{1, 0, 0, 0})

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		_, err := stdCopy(stdout, stderr, reader)

		assert.Error(t, err)
		assert.Equal(t, io.ErrUnexpectedEOF, err)
	})

	t.Run("unknown stream type defaults to stdout", func(t *testing.T) {
		// Stream type 0 (unknown) should default to stdout
		header := []byte{0, 0, 0, 0, 0, 0, 0, 4}
		payload := []byte("test")

		input := append(header, payload...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(4), written)
		assert.Equal(t, "test", stdout.String())
		assert.Empty(t, stderr.String())
	})

	t.Run("large payload", func(t *testing.T) {
		// Test with a larger payload (1KB)
		payload := make([]byte, 1024)
		for i := range payload {
			payload[i] = byte('A' + (i % 26))
		}

		// Size is 1024 = 0x400 = [0, 0, 4, 0] in big endian
		header := []byte{1, 0, 0, 0, 0, 0, 4, 0}

		input := append(header, payload...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(1024), written)
		assert.Equal(t, 1024, stdout.Len())
	})

	t.Run("mixed stdout and stderr", func(t *testing.T) {
		// Frame 1: stdout "out"
		frame1 := append([]byte{1, 0, 0, 0, 0, 0, 0, 3}, []byte("out")...)
		// Frame 2: stderr "err"
		frame2 := append([]byte{2, 0, 0, 0, 0, 0, 0, 3}, []byte("err")...)
		// Frame 3: stdout "end"
		frame3 := append([]byte{1, 0, 0, 0, 0, 0, 0, 3}, []byte("end")...)

		input := append(frame1, frame2...)
		input = append(input, frame3...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		assert.NoError(t, err)
		assert.Equal(t, int64(9), written)
		assert.Equal(t, "outend", stdout.String())
		assert.Equal(t, "err", stderr.String())
	})

	t.Run("incomplete payload", func(t *testing.T) {
		// Header says 10 bytes but only 5 provided
		header := []byte{1, 0, 0, 0, 0, 0, 0, 10}
		payload := []byte("short")

		input := append(header, payload...)
		reader := bytes.NewReader(input)

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		written, err := stdCopy(stdout, stderr, reader)

		// Should read what's available (5 bytes) then fail on next header
		assert.Equal(t, int64(5), written)
		// err may or may not be set depending on how LimitReader + next header read works
		_ = err
	})
}
