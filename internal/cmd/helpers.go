package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
)

// DefaultOperationTimeout is the default timeout for Docker operations.
const DefaultOperationTimeout = 30 * time.Second

// withDockerClient executes a function with a Docker client and default timeout context.
// Use withDockerClientContext for custom context handling.
func withDockerClient(fn func(ctx context.Context, client *docker.Client) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultOperationTimeout)
	defer cancel()

	return withDockerClientContext(ctx, func(client *docker.Client) error {
		return fn(ctx, client)
	})
}

// withDockerClientContext executes a function with a Docker client and custom context.
// The context is used for cancellation and timeout control.
func withDockerClientContext(ctx context.Context, fn func(*docker.Client) error) error {
	client, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer client.Close()

	return fn(client)
}

// parseJSONHeaders parses a JSON string into a map of HTTP headers.
// Returns nil if the input is empty. Logs a warning and returns nil on invalid JSON.
func parseJSONHeaders(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		log.Warn().Err(err).Msg("BOSUN_WEBHOOK_HEADERS contains invalid JSON; ignoring")
		return nil
	}
	return headers
}
