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
}
