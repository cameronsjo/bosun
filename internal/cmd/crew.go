package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Display limits for crew commands.
const (
	// MaxPortDisplayLength is the maximum length for displaying port mappings before truncation.
	MaxPortDisplayLength = 40
	// TruncatedPortLength is the length to truncate port display to when exceeding max.
	TruncatedPortLength = 37
	// DefaultLogTailLines is the default number of log lines to show.
	DefaultLogTailLines = 100
)

var (
	crewAll    bool
	crewTail   int
	crewFollow bool
)

var crewCmd = &cobra.Command{
	Use:     "crew",
	Aliases: []string{"scallywags"},
	Short:   "Manage containers",
	Long: `Crew commands for managing individual containers.

Commands:
  list      Show all hands on deck (docker ps)
  logs      Tail crew member logs
  inspect   Detailed crew info
  restart   Send crew member for coffee break`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var crewListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all hands on deck (docker ps)",
	Long:  `Lists all containers with their status, ports, and health.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return withDockerClient(func(ctx context.Context, client *docker.Client) error {
			containers, err := client.ListContainers(ctx, !crewAll)
			if err != nil {
				return fmt.Errorf("list containers: %w", err)
			}

			if len(containers) == 0 {
				ui.Warning("No containers found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tPORTS")
			fmt.Fprintln(w, "----\t------\t-----")

			for _, c := range containers {
				ports := strings.Join(c.Ports, ", ")
				if len(ports) > MaxPortDisplayLength {
					ports = ports[:TruncatedPortLength] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", c.Name, c.Status, ports)
			}

			w.Flush()
			return nil
		})
	},
}

var crewLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Tail crew member logs",
	Long:  `Shows logs from a container. Use -f to follow.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		// NOTE: This command uses explicit Docker client handling because it needs
		// custom context management for signal-based cancellation during log streaming.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle interrupt with panic recovery
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Log panic but don't crash - just cancel the context
					cancel()
				}
			}()
			<-sigCh
			cancel()
		}()

		return withDockerClientContext(ctx, func(client *docker.Client) error {
			reader, err := client.Logs(ctx, name, crewTail, crewFollow)
			if err != nil {
				return fmt.Errorf("get logs: %w", err)
			}

			// Stream logs to stdout, stripping Docker multiplex headers
			_, copyErr := stdCopy(os.Stdout, os.Stderr, reader)

			// Always close reader and capture error
			closeErr := reader.Close()

			// Handle copy errors first
			if copyErr != nil {
				if ctx.Err() != nil {
					// Context cancelled, normal exit
					return nil
				}
				return fmt.Errorf("read logs: %w", copyErr)
			}

			// Report close errors (usually less critical but shouldn't be silent)
			if closeErr != nil {
				ui.Warning("Failed to close log reader: %v", closeErr)
			}

			return nil
		})
	},
}

var crewInspectCmd = &cobra.Command{
	Use:   "inspect <name>",
	Short: "Detailed crew info",
	Long:  `Shows detailed information about a container.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		return withDockerClient(func(ctx context.Context, client *docker.Client) error {
			details, err := client.Inspect(ctx, name)
			if err != nil {
				return fmt.Errorf("inspect container: %w", err)
			}

			// Print as formatted JSON
			output, err := json.MarshalIndent(details, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal details: %w", err)
			}

			fmt.Println(string(output))
			return nil
		})
	},
}

var crewRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Send crew member for coffee break",
	Long:  `Restarts a specific container.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		return withDockerClient(func(ctx context.Context, client *docker.Client) error {
			ui.Blue.Printf("Sending %s for a coffee break...\n", name)

			if err := client.RestartContainer(ctx, name); err != nil {
				return fmt.Errorf("restart container: %w", err)
			}

			ui.Success("%s is back on duty!", name)
			return nil
		})
	},
}

// stdCopy copies docker multiplexed stream to stdout/stderr.
// Docker log streams have an 8-byte header per frame:
// [STREAM_TYPE, 0, 0, 0, SIZE1, SIZE2, SIZE3, SIZE4]
// Where STREAM_TYPE is 1 for stdout, 2 for stderr.
func stdCopy(stdout, stderr io.Writer, src io.Reader) (written int64, err error) {
	buf := make([]byte, 32*1024)
	header := make([]byte, 8)

	for {
		// Read header
		_, err := io.ReadFull(src, header)
		if err != nil {
			if err == io.EOF {
				return written, nil
			}
			return written, err
		}

		// Parse size from header (big endian)
		size := int64(header[4])<<24 | int64(header[5])<<16 | int64(header[6])<<8 | int64(header[7])

		// Select output based on stream type
		var dst io.Writer
		switch header[0] {
		case 1:
			dst = stdout
		case 2:
			dst = stderr
		default:
			dst = stdout
		}

		// Copy frame content
		n, err := io.CopyBuffer(dst, io.LimitReader(src, size), buf)
		written += n
		if err != nil {
			return written, err
		}
	}
}

func init() {
	crewListCmd.Flags().BoolVarP(&crewAll, "all", "a", false, "Show all containers (including stopped)")

	crewLogsCmd.Flags().IntVarP(&crewTail, "tail", "n", DefaultLogTailLines, "Number of lines to show")
	crewLogsCmd.Flags().BoolVarP(&crewFollow, "follow", "f", false, "Follow log output")

	crewCmd.AddCommand(crewListCmd)
	crewCmd.AddCommand(crewLogsCmd)
	crewCmd.AddCommand(crewInspectCmd)
	crewCmd.AddCommand(crewRestartCmd)

	rootCmd.AddCommand(crewCmd)
}
